package cel

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// GraphQL parsing limits. They bound the work graphql() does on attacker-
// controlled input; exceeding any of them marks the request invalid, which a
// policy treats as "deny" (fail closed).
const (
	// graphqlMaxTokens caps the lexer token count of the query document.
	graphqlMaxTokens = 100_000
	// graphqlMaxFragmentDepth caps nested fragment spreads / inline fragments
	// while expanding an operation's root selection set.
	graphqlMaxFragmentDepth = 32
	// graphqlMaxRootFields caps the total number of root fields produced by
	// expansion. A fragment spread used repeatedly can multiply the field count
	// exponentially; this bound keeps expansion linear in practice.
	graphqlMaxRootFields = 1000
)

// GraphQL operation type names, as reported in operations[].type.
const (
	graphqlQuery        = "query"
	graphqlMutation     = "mutation"
	graphqlSubscription = "subscription"
)

// ParseGraphQLRequest parses a GraphQL-over-HTTP request body (the decoded JSON
// object {"query": ..., "variables": ..., "operationName": ...}) and returns a
// structured, JSON-shaped view of it for policy evaluation:
//
//	{
//	  "valid": bool,          // false on any input or parse problem
//	  "error": string,        // "" when valid
//	  "operations": [{"type": "query"|"mutation"|"subscription", "name": string, "fields": [field]}],
//	  "queries":       [field], // root fields of every query operation
//	  "mutations":     [field], // root fields of every mutation operation
//	  "subscriptions": [field], // root fields of every subscription operation
//	}
//	field = {"name": string, "alias": string, "args": map}
//
// Every operation in the document is reported regardless of operationName, so a
// policy sees a mutation even when the client asks the server to run a
// different operation from the same document. Fragment spreads and inline
// fragments in an operation's root selection set are expanded; type conditions
// and directives (@skip/@include) are ignored, so a conditionally skipped field
// is still reported. Arguments have variables substituted from "variables",
// falling back to the variable's default value; a variable with neither is
// omitted from an object (or null inside a list).
//
// It never returns an error: malformed input yields valid=false with empty
// operation and field lists, so `.exists()` over them is always safe.
func ParseGraphQLRequest(body any) map[string]any {
	result, err := parseGraphQLRequest(body)
	if err != nil {
		return graphqlInvalid(err.Error())
	}
	return result
}

func graphqlInvalid(msg string) map[string]any {
	return map[string]any{
		"valid":         false,
		"error":         msg,
		"operations":    []any{},
		"queries":       []any{},
		"mutations":     []any{},
		"subscriptions": []any{},
	}
}

func parseGraphQLRequest(body any) (map[string]any, error) {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("graphql: request body is not a JSON object")
	}
	rawQuery, ok := obj["query"]
	if !ok {
		return nil, errors.New("graphql: request body has no \"query\" field")
	}
	query, ok := rawQuery.(string)
	if !ok {
		return nil, errors.New("graphql: \"query\" is not a string")
	}

	var variables map[string]any
	switch v := obj["variables"].(type) {
	case nil:
	case map[string]any:
		variables = v
	default:
		return nil, errors.New("graphql: \"variables\" is not a JSON object")
	}

	doc, err := parser.ParseQueryWithTokenLimit(&ast.Source{Input: query}, graphqlMaxTokens)
	if err != nil {
		return nil, fmt.Errorf("graphql: parse: %s", err.Error())
	}
	if len(doc.Operations) == 0 {
		return nil, errors.New("graphql: document contains no operations")
	}

	fragments := make(map[string]*ast.FragmentDefinition, len(doc.Fragments))
	for _, f := range doc.Fragments {
		if _, dup := fragments[f.Name]; dup {
			return nil, fmt.Errorf("graphql: duplicate fragment %q", f.Name)
		}
		fragments[f.Name] = f
	}

	operations := make([]any, 0, len(doc.Operations))
	byType := map[string][]any{
		graphqlQuery:        {},
		graphqlMutation:     {},
		graphqlSubscription: {},
	}
	total := 0
	for _, op := range doc.Operations {
		opType := string(op.Operation)
		if opType == "" {
			opType = graphqlQuery // shorthand `{ ... }` is a query
		}
		if _, known := byType[opType]; !known {
			return nil, fmt.Errorf("graphql: unknown operation type %q", opType)
		}

		var rootFields []*ast.Field
		if err := expandRootSelections(op.SelectionSet, fragments, map[string]bool{}, 0, &rootFields, &total); err != nil {
			return nil, err
		}

		vars := newVarResolver(op.VariableDefinitions, variables)
		fields := make([]any, 0, len(rootFields))
		for _, f := range rootFields {
			field := map[string]any{
				"name":  f.Name,
				"alias": fieldAlias(f),
				"args":  vars.arguments(f.Arguments),
			}
			fields = append(fields, field)
			byType[opType] = append(byType[opType], field)
		}
		operations = append(operations, map[string]any{
			"type":   opType,
			"name":   op.Name,
			"fields": fields,
		})
	}

	return map[string]any{
		"valid":         true,
		"error":         "",
		"operations":    operations,
		"queries":       byType[graphqlQuery],
		"mutations":     byType[graphqlMutation],
		"subscriptions": byType[graphqlSubscription],
	}, nil
}

// fieldAlias returns the field's alias, or "" when it has none. gqlparser sets
// Alias to the field name when no alias is written.
func fieldAlias(f *ast.Field) string {
	if f.Alias == f.Name {
		return ""
	}
	return f.Alias
}

// expandRootSelections flattens a root selection set into its fields,
// expanding fragment spreads and inline fragments. visiting tracks the fragment
// names on the current expansion path to detect cycles.
func expandRootSelections(
	set ast.SelectionSet,
	fragments map[string]*ast.FragmentDefinition,
	visiting map[string]bool,
	depth int,
	out *[]*ast.Field,
	total *int,
) error {
	if depth > graphqlMaxFragmentDepth {
		return fmt.Errorf("graphql: fragment nesting exceeds %d levels", graphqlMaxFragmentDepth)
	}
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			*total++
			if *total > graphqlMaxRootFields {
				return fmt.Errorf("graphql: more than %d root fields", graphqlMaxRootFields)
			}
			*out = append(*out, s)
		case *ast.InlineFragment:
			if err := expandRootSelections(s.SelectionSet, fragments, visiting, depth+1, out, total); err != nil {
				return err
			}
		case *ast.FragmentSpread:
			def, ok := fragments[s.Name]
			if !ok {
				return fmt.Errorf("graphql: unknown fragment %q", s.Name)
			}
			if visiting[s.Name] {
				return fmt.Errorf("graphql: fragment cycle through %q", s.Name)
			}
			visiting[s.Name] = true
			err := expandRootSelections(def.SelectionSet, fragments, visiting, depth+1, out, total)
			delete(visiting, s.Name)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("graphql: unsupported selection %T", sel)
		}
	}
	return nil
}

// varResolver substitutes variable references in argument values.
type varResolver struct {
	defs   map[string]*ast.VariableDefinition
	values map[string]any
}

func newVarResolver(defs ast.VariableDefinitionList, values map[string]any) *varResolver {
	m := make(map[string]*ast.VariableDefinition, len(defs))
	for _, d := range defs {
		m[d.Variable] = d
	}
	return &varResolver{defs: m, values: values}
}

// lookup resolves a variable: the supplied value (even an explicit null), then
// the definition's default, else unset.
func (r *varResolver) lookup(name string) (any, bool) {
	if v, ok := r.values[name]; ok {
		return v, true
	}
	if d, ok := r.defs[name]; ok && d.DefaultValue != nil {
		// Defaults are constant values; they cannot reference variables.
		return r.value(d.DefaultValue)
	}
	return nil, false
}

func (r *varResolver) arguments(args ast.ArgumentList) map[string]any {
	out := make(map[string]any, len(args))
	for _, a := range args {
		if v, ok := r.value(a.Value); ok {
			out[a.Name] = v
		}
	}
	return out
}

// value converts a GraphQL value literal to a JSON-shaped Go value. The bool
// result is false only for an unset variable, which callers omit.
func (r *varResolver) value(v *ast.Value) (any, bool) {
	if v == nil {
		return nil, true
	}
	switch v.Kind {
	case ast.Variable:
		return r.lookup(v.Raw)
	case ast.IntValue:
		if n, err := strconv.ParseInt(v.Raw, 10, 64); err == nil {
			return n, true
		}
		// Out of int64 range: keep the literal text rather than guess.
		return v.Raw, true
	case ast.FloatValue:
		if f, err := strconv.ParseFloat(v.Raw, 64); err == nil {
			return f, true
		}
		return v.Raw, true
	case ast.StringValue, ast.BlockValue, ast.EnumValue:
		return v.Raw, true
	case ast.BooleanValue:
		return v.Raw == "true", true
	case ast.NullValue:
		return nil, true
	case ast.ListValue:
		list := make([]any, 0, len(v.Children))
		for _, c := range v.Children {
			item, ok := r.value(c.Value)
			if !ok {
				item = nil
			}
			list = append(list, item)
		}
		return list, true
	case ast.ObjectValue:
		obj := make(map[string]any, len(v.Children))
		for _, c := range v.Children {
			if item, ok := r.value(c.Value); ok {
				obj[c.Name] = item
			}
		}
		return obj, true
	default:
		return v.Raw, true
	}
}

// refToNative converts a CEL value (as received by a function binding) back to
// a JSON-shaped Go value: string, int64, uint64, float64, bool, nil, []any, or
// map[string]any. Values that have no JSON shape convert to nil.
func refToNative(v ref.Val) any {
	switch vv := v.(type) {
	case nil:
		return nil
	case types.String:
		return string(vv)
	case types.Int:
		return int64(vv)
	case types.Uint:
		return uint64(vv)
	case types.Double:
		return float64(vv)
	case types.Bool:
		return bool(vv)
	case types.Null:
		return nil
	case traits.Mapper:
		out := map[string]any{}
		for it := vv.Iterator(); it.HasNext() == types.True; {
			k := it.Next()
			ks, ok := k.(types.String)
			if !ok {
				// JSON objects only have string keys; anything else is not a
				// GraphQL request body.
				return nil
			}
			out[string(ks)] = refToNative(vv.Get(k))
		}
		return out
	case traits.Lister:
		var out []any
		for it := vv.Iterator(); it.HasNext() == types.True; {
			out = append(out, refToNative(it.Next()))
		}
		if out == nil {
			out = []any{}
		}
		return out
	default:
		return nil
	}
}

// graphqlVal runs ParseGraphQLRequest over a CEL argument and adapts the result
// back into a CEL map.
func graphqlVal(arg ref.Val) ref.Val {
	return types.DefaultTypeAdapter.NativeToValue(ParseGraphQLRequest(refToNative(arg)))
}

// originalGraphQLBody picks the body to parse for the two-argument form
// graphql(lowered, original). The engine injects _originalParams.<field> as the
// second argument, but when the user's argument is not a plain params path the
// rewriter injects the whole _originalParams map instead. Use the second
// argument only when it is demonstrably the original-case form of the first —
// both carry a "query" string and lowering the original yields the first —
// otherwise parse the first argument as given.
func originalGraphQLBody(lhs, rhs ref.Val) ref.Val {
	lq, ok := graphqlQueryOf(lhs)
	if !ok {
		return lhs
	}
	rq, ok := graphqlQueryOf(rhs)
	if !ok {
		return lhs
	}
	if rq == lq || strings.ToLower(rq) == lq {
		return rhs
	}
	return lhs
}

// graphqlQueryOf returns the "query" string of a CEL map value.
func graphqlQueryOf(v ref.Val) (string, bool) {
	m, ok := v.(traits.Mapper)
	if !ok {
		return "", false
	}
	q, found := m.Find(types.String("query"))
	if !found {
		return "", false
	}
	s, ok := q.(types.String)
	return string(s), ok
}
