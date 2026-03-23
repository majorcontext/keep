CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL,
  password TEXT NOT NULL,
  role TEXT NOT NULL
);

INSERT INTO users VALUES (1,  'Alice Chen',     'alice@company.com',   'hunter2',      'admin');
INSERT INTO users VALUES (2,  'Bob Park',       'bob@company.com',     'p@ssw0rd!',    'editor');
INSERT INTO users VALUES (3,  'Carol White',    'carol@company.com',   'letmein123',   'viewer');
INSERT INTO users VALUES (4,  'Dan Rivera',     'dan@company.com',     'qwerty456',    'editor');
INSERT INTO users VALUES (5,  'Eve Foster',     'eve@company.com',     'trustno1!',    'admin');
INSERT INTO users VALUES (6,  'Frank Zhao',     'frank@company.com',   'baseball9',    'viewer');
INSERT INTO users VALUES (7,  'Grace Kim',      'grace@company.com',   'shadow99!',    'editor');
INSERT INTO users VALUES (8,  'Hank Patel',     'hank@company.com',    'dragon123',    'viewer');
INSERT INTO users VALUES (9,  'Iris Novak',     'iris@company.com',    'master!42',    'admin');
INSERT INTO users VALUES (10, 'Jack Torres',    'jack@company.com',    'abc123xyz',    'editor');
INSERT INTO users VALUES (11, 'Karen Liu',      'karen@company.com',   'welcome1!',    'viewer');
INSERT INTO users VALUES (12, 'Leo Santos',     'leo@company.com',     'passw0rd!',    'editor');
