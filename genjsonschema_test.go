package genjsonschema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v2"
)

func stubSchema(id string, p property) *schema {
	return &schema{ID: id, JsonSchemaRef: jsonSchemaRef, property: p}
}

func pbool(v bool) *bool {
	return &v
}

func TestSchemaGeneration(t *testing.T) {
	tests := []struct {
		name                 string
		given                string
		additionalProperties bool
		requireAllProperties bool
		want                 *schema
	}{

		{"list",
			"- 42",
			true,
			false,
			stubSchema("", property{
				Type: typeArray,
				Items: &items{
					AnyOf: propertyList{
						&property{
							Type: typeInteger,
						},
					},
				},
			}),
		},
		{"simple",
			"foo: 'bar'",
			true,
			false,
			stubSchema("", property{
				Type: typeObject,
				Properties: &properties{
					"foo": &property{
						Type: typeString,
					},
				},
			}),
		},
		{"datatypes",
			"" +
				"str: 'bar'\n" +
				"int: 42\n" +
				"float: 42.2\n" +
				"nil: ~\n" +
				"obj: {}\n" +
				"arr: []\n",
			true,
			false,
			stubSchema("", property{
				Type: typeObject,
				Properties: &properties{
					"str": &property{
						Type: typeString,
					},
					"int": &property{
						Type: typeInteger,
					},
					"float": &property{
						Type: typeNumber,
					},
					"nil": &property{
						Type: typeNull,
					},
					"obj": &property{
						Type:       typeObject,
						Properties: &properties{},
					},
					"arr": &property{
						Type:  typeArray,
						Items: &items{AnyOf: propertyList{}},
					},
				},
			}),
		},
		{"nested",
			"" +
				"foo:\n" +
				"  bar: 42\n",
			true,
			false,
			stubSchema("", property{
				Type: typeObject,
				Properties: &properties{
					"foo": &property{
						Type: typeObject,
						Properties: &properties{
							"bar": &property{
								Type: typeInteger,
							},
						},
					},
				},
			}),
		},
		{"nested with requiring attributes",
			"" +
				"foo:\n" +
				"  bar: 42\n",
			true,
			true,
			stubSchema("", property{
				Type:     typeObject,
				Required: []string{"foo"},
				Properties: &properties{
					"foo": &property{
						Type:     typeObject,
						Required: []string{"bar"},
						Properties: &properties{
							"bar": &property{
								Type: typeInteger,
							},
						},
					},
				},
			}),
		},
		{"nested with additional attributes",
			"" +
				"foo:\n" +
				"  bar: 42\n",
			true,
			false,
			stubSchema("", property{
				Type:                 typeObject,
				AdditionalProperties: nil, // true is default and thus we expect nil to save schema size
				Properties: &properties{
					"foo": &property{
						Type:                 typeObject,
						AdditionalProperties: nil,
						Properties: &properties{
							"bar": &property{
								Type: typeInteger,
							},
						},
					},
				},
			}),
		},
		{"nested without additional attributes",
			"" +
				"foo:\n" +
				"  bar: 42\n",
			false,
			false,
			stubSchema("", property{
				Type:                 typeObject,
				AdditionalProperties: pbool(false), // false must be declared explicitely by the schema
				Properties: &properties{
					"foo": &property{
						Type:                 typeObject,
						AdditionalProperties: pbool(false),
						Properties: &properties{
							"bar": &property{
								Type: typeInteger,
							},
						},
					},
				},
			}),
		},
		{"array",
			"" +
				"items: [1]\n",
			true,
			false,
			stubSchema("", property{
				Type: typeObject,
				Properties: &properties{
					"items": &property{
						Type: typeArray,
						Items: &items{
							AnyOf: propertyList{
								&property{Type: typeInteger},
							},
						},
					},
				},
			}),
		},
		{"array-duplicate-elimination",
			"" +
				"items: [1 , 2, {\"foo\": 1}, {\"foo\": 2}, {\"bar\": 3}]\n", //items at index 0,1 and 2,3 are identical types
			true,
			false,
			stubSchema("", property{
				Type: typeObject,
				Properties: &properties{
					"items": &property{
						Type: typeArray,
						Items: &items{ //items from above with removed indices
							AnyOf: propertyList{
								&property{Type: typeInteger},
								&property{Type: typeObject,
									Properties: &properties{
										"foo": &property{
											Type: typeInteger,
										},
									},
								},
								&property{Type: typeObject,
									Properties: &properties{
										"bar": &property{
											Type: typeInteger,
										},
									},
								},
							},
						},
					},
				},
			}),
		},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			var given interface{}
			if err := yaml.Unmarshal([]byte(v.given), &given); err != nil {
				t.Fatal(err)
			}

			got, err := newSchemaFromYAML([]byte(v.given), NewSchemaConfig("", v.additionalProperties, v.requireAllProperties))
			if err != nil {
				t.Error(err)
			}
			if delta := cmp.Diff(got, v.want, cmp.AllowUnexported(schema{})); delta != "" {
				t.Logf("Given %s got %v but wanted %v\nDelta:\n", v.given, got, v.want)
				t.Error(delta)
			}

		})

	}
}

func TestSchemaGenerationFromJSONEqualsYAML(t *testing.T) {
	tests := []struct {
		name      string
		givenJSON string
		givenYAML string
	}{
		{
			"list",
			"[1,2,3]",
			"" +
				"- 1\n" +
				"- 2\n" +
				"- 3\n",
		},
		{
			"single boolean",
			"true",
			"true",
		},
		{
			"nested object",
			"{\"foo\":{\"bar\":42}}",
			"" +
				"foo:\n" +
				"  bar: 42\n",
		},
	}

	for _, v := range tests {
		t.Run(
			v.name, func(t *testing.T) {
				gotFromJSON, err := GenerateFromJSON([]byte(v.givenJSON), nil)
				if err != nil {
					t.Error(err)
				}
				gotFromYAML, err := GenerateFromYAML([]byte(v.givenYAML), nil)
				if err != nil {
					t.Error(err)
				}
				if diff := cmp.Diff(gotFromJSON, gotFromYAML); diff != "" {
					t.Error(diff)
				}
			},
		)

	}

}

func TestSerialization(t *testing.T) {
	tests := []struct {
		name  string
		given *schema
		want  string
	}{
		{
			name: "single object with attribute foo and id bar",
			given: stubSchema("bar", property{
				Type: typeObject,
				Properties: &properties{
					"foo": &property{
						Type: typeString,
					},
				},
			}),
			want: `
		{
	  		"$schema": "http://json-schema.org/draft-07/schema",
			"$id": "bar",
	  		"properties": {
    			"foo": {
      				"type": "string"
			    }
  			},
  			"type": "object"
		}
		`},
		{
			name: "single object, all attributes required no addtional attributes",
			given: stubSchema("", property{
				Type:                 typeObject,
				AdditionalProperties: pbool(false),
				Required:             []string{"foo"},
				Properties: &properties{
					"foo": &property{
						Type: typeString,
					},
				},
			}),
			want: `
		{
	  		"$schema": "http://json-schema.org/draft-07/schema",
	  		"properties": {
    			"foo": {
      				"type": "string"
			    }
  			},
  			"type": "object",
			"additionalProperties": false,  
			"required": ["foo"]
		}
		`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(test.given)
			if err != nil {
				t.Fatal(err)
			}
			// unmarshal again so we don't need to compare strings
			gotAsMap := make(map[string]interface{})
			wantedAsMap := make(map[string]interface{})

			if err = json.Unmarshal(got, &gotAsMap); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal([]byte(test.want), &wantedAsMap); err != nil {
				t.Fatal(err)
			}
			if delta := cmp.Diff(gotAsMap, wantedAsMap); delta != "" {
				t.Logf("Given %v got %v but wanted %v\nDelta:\n", test.given, string(got), test.want)
				t.Error(delta)
			}
		})
	}
}

func TestRejectSpecialYAML(t *testing.T) {
	given := `42: "not supported"`
	_, err := GenerateFromYAML([]byte(given), nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("Expected error but got %v", err)
	}
}
