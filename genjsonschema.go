/*
Package genjsonschema implements a simple JSON Schema generator.
It generates schemas in accordance with https://json-schema.org/draft-07/schema.
It supports json and a subset of YAML (notably, mappings may only have string keys).


Lists will always be defined using the anyOf keyword and won't be limited on item numbers.
A schema generated from [1, true] will thus accept a list with an undefined number of integers,
booleans, and combinations thereof, but will reject other element types such as string.

Example:
	from := []byte("{'foo': 'bar'}")
	schema, err := GenerateFromJSON(from, nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(schema)

	# prints {"$schema":"http://json-schema.org/draft-07/schema","additionalProperties":false,"properties":{"foo":{"type":"string"}},"type":"object","required":["foo"]}
*/
package genjsonschema

import (
	"encoding/json"
	"fmt"
	"reflect"

	"gopkg.in/yaml.v2"
)

const jsonSchemaRef = "http://json-schema.org/draft-07/schema"

// SchemaConfig holds configuration used when generating a schema
type SchemaConfig struct {
	ID                   string // $id field value of the schema, omitted if empty
	AdditionalProperties bool   // Whether the schema allow objects to have previously unknown properties
	RequireAllProperties bool   // Whether the schema requires taht all properties of an object are set
}

// NewSchemaConfig returns a new SchemaConfig.
// See SchemaConfig for details on the meaning of the arguments.
func NewSchemaConfig(id string, additionalProperties, requireAllProperties bool) *SchemaConfig {
	return &SchemaConfig{
		ID:                   id,
		AdditionalProperties: additionalProperties,
		RequireAllProperties: requireAllProperties,
	}
}

// NewDefaultSchemaConfig generates a default SchemaConfig.
// It sets additionalProperties to false and requireAllProperties to true, thus
// requiring objects to have exactly the properties encountered on schema generation.
func NewDefaultSchemaConfig() *SchemaConfig {
	return NewSchemaConfig("", false, true)
}

// GenerateFromJSON generates a JSON Schema from json.
// If schemaConfig is nil, a NewDefaultSchemaConfig will be used.
func GenerateFromJSON(json []byte, schemaConfig *SchemaConfig) ([]byte, error) {
	return GenerateFromYAML(json, schemaConfig) // YAML is a superset of JSON
}

// GenerateFromYAML generates a JSON Schema from yaml.
// It requires that all mapping keys are strings, i.e. the following is fine:
//   foo: "bar"  # ok because "foo" is of type string
// but the following is not fine:
//   42: "bar"  # not ok because 42 is an integer
//
// If schemaConfig is nil, NewDefaultSchemaConfig will be used.
func GenerateFromYAML(yaml []byte, schemaConfig *SchemaConfig) ([]byte, error) {
	schema, err := newSchemaFromYAML(yaml, schemaConfig)
	if err != nil {
		return []byte{}, err
	}
	return schema.Marshal()
}

// Schema represents a json schema. Exported fields correspond to attributes of the schema
// whereas unexported fields are internally used when generating the schema.
type schema struct {
	JsonSchemaRef  string `json:"$schema"`
	SchemaEntryRef string `json:"$ref,omitempty"`
	ID             string `json:"$id,omitempty"`
	property       `json:",omitempty"`
}

// newSchemaFromYAML generates a Schema from yaml input.
// See GenerateFromYaml() for information about the subset of yaml that is supported.
// If schemaConfig is nil, a NewDefaultSchemaConfig() will be used.
func newSchemaFromYAML(b []byte, schemaConfig *SchemaConfig) (*schema, error) {
	if schemaConfig == nil {
		schemaConfig = NewDefaultSchemaConfig()
	}

	var data interface{}
	if err := yaml.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	return generateSchema(data, schemaConfig)
}

func generateSchema(from interface{}, config *SchemaConfig) (*schema, error) {
	s := &schema{
		JsonSchemaRef: jsonSchemaRef,
		ID:            config.ID,
	}
	p, err := newProperty(from, config)
	if err != nil {
		return nil, err
	}
	s.property = *p
	return s, nil
}

// Marshal returns the json encoding of the schema.
func (s *schema) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

type properties map[string]*property

type property struct {
	AdditionalProperties *bool       `json:"additionalProperties,omitempty"`
	Items                *items      `json:"items,omitempty"`
	Properties           *properties `json:"properties,omitempty"`
	Type                 jsonType    `json:"type,omitempty"`
	Required             []string    `json:"required,omitempty"`
}

type propertyList []*property

// withoutDuplicates returns a PropertyList without duplicate entries.
func (p *propertyList) withoutDuplicates() propertyList {
	unique := make([]*property, 0)
	for _, v := range *p {
		if !v.equalsOneOf(unique) {
			unique = append(unique, v)
		}
	}
	return unique
}

type items struct {
	AnyOf propertyList `json:"anyOf"`
}

type jsonType string // Type holds the datatypes known to jsonschema

const (
	typeObject  jsonType = "object"
	typeArray   jsonType = "array"
	typeString  jsonType = "string"
	typeNumber  jsonType = "number"
	typeInteger jsonType = "integer"
	typeBoolean jsonType = "boolean"
	typeNull    jsonType = "null"
)

func (p *property) equalsOneOf(others []*property) bool {
	for _, compare := range others {
		if p.Type == compare.Type {
			if compare.Type == typeObject { //deep compare needed in case of objects
				if !reflect.DeepEqual(p, compare) {
					continue
				}
			}
			return true
		}
	}
	return false
}

func (p *property) requireExactlyAllKeysFromMap(m map[string]interface{}) {
	p.Required = make([]string, 0, len(m))
	for k := range m {
		p.Required = append(p.Required, k)
	}
}

func newProperty(data interface{}, config *SchemaConfig) (*property, error) {
	p := &property{}

	// helper function to set pointers to primitive types
	pbool := func(b bool) *bool { return &b }

	// pre-convert keys to strings to prevent code duplication below
	switch v := data.(type) {
	case map[interface{}]interface{}:
		var err error
		data, err = convertMap(v)
		if err != nil {
			return nil, err
		}
	}

	switch v := data.(type) {
	case map[string]interface{}:
		if config.RequireAllProperties {
			p.requireExactlyAllKeysFromMap(v)
		}
		p.Type = typeObject
		if !config.AdditionalProperties { // default is true, so only set it in other case
			p.AdditionalProperties = pbool(false)
		}
		if err := p.addObject(v, config); err != nil {
			return nil, err
		}
		return p, nil
	case []interface{}:
		p.Type = typeArray
		if err := p.addArray(v, config); err != nil {
			return nil, err
		}
		return p, nil
	case string:
		p.Type = typeString
		return p, nil
	case int, int8, int16, int32, int64:
		p.Type = typeInteger
		return p, nil
	case float32, float64:
		p.Type = typeNumber
		return p, nil
	case bool:
		p.Type = typeBoolean
		return p, nil
	case nil:
		p.Type = typeNull
		return p, nil
	default:
		return nil, fmt.Errorf("unexpected type %v of data", reflect.TypeOf(data))
	}
}

func convertMap(values map[interface{}]interface{}) (map[string]interface{}, error) {
	converted := make(map[string]interface{})
	for k, v := range values {
		ident, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("unsupported type %s of object key, only string keys are supported", reflect.TypeOf(k))
		}
		converted[ident] = v
	}
	return converted, nil
}

func (p *property) addObject(values map[string]interface{}, config *SchemaConfig) error {
	if p.Properties == nil {
		properties := make(properties)
		p.Properties = &properties
	}
	for k, v := range values {
		var err error
		(*p.Properties)[k], err = newProperty(v, config)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *property) addArray(values []interface{}, config *SchemaConfig) error {
	if p.Items == nil {
		p.Items = &items{AnyOf: []*property{}}
	}
	for _, v := range values {
		item, err := newProperty(v, config)
		if err != nil {
			return err
		}
		p.Items.AnyOf = append(p.Items.AnyOf, item)
	}
	p.Items.AnyOf = p.Items.AnyOf.withoutDuplicates()
	return nil
}
