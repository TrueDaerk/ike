package graphql

// testdata_test.go holds the fixture every test in this package works from: a
// miniature Star-Wars-shaped schema, in the exact shape an introspection
// response has. Sharing one fixture keeps the tests honest about the whole
// path — a change to the introspection parse shows up in the SDL, the caret
// analysis and the cache tests at once.

const introspectionFixture = `{
  "data": {
    "__schema": {
      "queryType": {"name": "Query"},
      "mutationType": {"name": "Mutation"},
      "subscriptionType": null,
      "types": [
        {
          "kind": "OBJECT",
          "name": "Query",
          "description": "The root of every read.",
          "fields": [
            {
              "name": "hero",
              "description": "The hero of an episode.",
              "isDeprecated": false,
              "args": [
                {"name": "episode", "description": "Which film.", "defaultValue": "NEWHOPE",
                 "type": {"kind": "ENUM", "name": "Episode", "ofType": null}}
              ],
              "type": {"kind": "OBJECT", "name": "Character", "ofType": null}
            },
            {
              "name": "search",
              "description": null,
              "isDeprecated": false,
              "args": [
                {"name": "text", "description": null, "defaultValue": null,
                 "type": {"kind": "NON_NULL", "name": null,
                          "ofType": {"kind": "SCALAR", "name": "String", "ofType": null}}},
                {"name": "first", "description": null, "defaultValue": "10",
                 "type": {"kind": "SCALAR", "name": "Int", "ofType": null}}
              ],
              "type": {"kind": "LIST", "name": null,
                       "ofType": {"kind": "OBJECT", "name": "Character", "ofType": null}}
            },
            {
              "name": "legacyHero",
              "description": null,
              "isDeprecated": true,
              "args": [],
              "type": {"kind": "OBJECT", "name": "Character", "ofType": null}
            }
          ],
          "inputFields": null, "interfaces": [], "enumValues": null, "possibleTypes": null
        },
        {
          "kind": "OBJECT",
          "name": "Mutation",
          "description": null,
          "fields": [
            {"name": "rename", "description": null, "isDeprecated": false,
             "args": [{"name": "name", "description": null, "defaultValue": null,
                       "type": {"kind": "NON_NULL", "name": null,
                                "ofType": {"kind": "SCALAR", "name": "String", "ofType": null}}}],
             "type": {"kind": "OBJECT", "name": "Character", "ofType": null}}
          ],
          "inputFields": null, "interfaces": [], "enumValues": null, "possibleTypes": null
        },
        {
          "kind": "OBJECT",
          "name": "Character",
          "description": "Someone in the films.",
          "fields": [
            {"name": "name", "description": null, "isDeprecated": false, "args": [],
             "type": {"kind": "NON_NULL", "name": null,
                      "ofType": {"kind": "SCALAR", "name": "String", "ofType": null}}},
            {"name": "homeworld", "description": null, "isDeprecated": false, "args": [],
             "type": {"kind": "OBJECT", "name": "Planet", "ofType": null}},
            {"name": "friends", "description": null, "isDeprecated": false, "args": [],
             "type": {"kind": "LIST", "name": null,
                      "ofType": {"kind": "OBJECT", "name": "Character", "ofType": null}}}
          ],
          "inputFields": null, "interfaces": [{"kind": "INTERFACE", "name": "Named", "ofType": null}],
          "enumValues": null, "possibleTypes": null
        },
        {
          "kind": "OBJECT",
          "name": "Planet",
          "description": null,
          "fields": [
            {"name": "name", "description": null, "isDeprecated": false, "args": [],
             "type": {"kind": "SCALAR", "name": "String", "ofType": null}}
          ],
          "inputFields": null, "interfaces": [], "enumValues": null, "possibleTypes": null
        },
        {
          "kind": "INTERFACE",
          "name": "Named",
          "description": null,
          "fields": [
            {"name": "name", "description": null, "isDeprecated": false, "args": [],
             "type": {"kind": "SCALAR", "name": "String", "ofType": null}}
          ],
          "inputFields": null, "interfaces": null, "enumValues": null,
          "possibleTypes": [{"kind": "OBJECT", "name": "Character", "ofType": null}]
        },
        {
          "kind": "ENUM",
          "name": "Episode",
          "description": null,
          "fields": null, "inputFields": null, "interfaces": null,
          "enumValues": [
            {"name": "NEWHOPE", "description": null, "isDeprecated": false},
            {"name": "EMPIRE", "description": null, "isDeprecated": false}
          ],
          "possibleTypes": null
        },
        {
          "kind": "SCALAR", "name": "String", "description": null,
          "fields": null, "inputFields": null, "interfaces": null,
          "enumValues": null, "possibleTypes": null
        },
        {
          "kind": "OBJECT", "name": "__Schema", "description": null,
          "fields": [], "inputFields": null, "interfaces": [],
          "enumValues": null, "possibleTypes": null
        }
      ]
    }
  }
}`

// fixtureSchema parses the fixture; every test starts from it.
func fixtureSchema() *Schema {
	s, err := ParseIntrospection([]byte(introspectionFixture))
	if err != nil {
		panic(err)
	}
	return s
}
