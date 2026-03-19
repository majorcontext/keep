package cel

import "strings"

func LowerFunc(s string) string {
	return strings.ToLower(s)
}

func UpperFunc(s string) string {
	return strings.ToUpper(s)
}

func MatchesDomainFunc(email string, domains []string) bool {
	// Extract domain from email
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	emailDomain := strings.ToLower(email[at+1:])

	for _, d := range domains {
		d = strings.ToLower(d)
		if emailDomain == d || strings.HasSuffix(emailDomain, "."+d) {
			return true
		}
	}
	return false
}
