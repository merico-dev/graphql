package graphql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/merico-dev/graphql/ident"
)

func ConstructQuery(v interface{}, variables map[string]interface{}) (string, map[string]interface{}) {
	query := query(v, variables)
	if len(variables) > 0 {
		newVariables := map[string]interface{}{}
		for k, v := range variables {
			if v2, ok := v.([]map[string]interface{}); ok {
				for index, subMap := range v2 {
					for subKey, subV := range subMap {
						newVariables[fmt.Sprintf(`%s__%d__%s`, k, index, subKey)] = subV
					}
				}
			} else {
				newVariables[k] = v
			}
		}
		return "query(" + queryArguments(newVariables) + ")" + query, newVariables
	}
	return query, variables
}

func ConstructMutation(v interface{}, variables map[string]interface{}) string {
	query := query(v, variables)
	if len(variables) > 0 {
		return "mutation(" + queryArguments(variables) + ")" + query
	}
	return "mutation" + query
}

// queryArguments constructs a minified arguments string for variables.
//
// E.g., map[string]interface{}{"a": Int(123), "b": NewBoolean(true)} -> "$a:Int!$b:Boolean".
func queryArguments(variables map[string]interface{}) string {
	// Sort keys in order to produce deterministic output for testing purposes.
	// TODO: If tests can be made to work with non-deterministic output, then no need to sort.
	keys := make([]string, 0, len(variables))
	for k := range variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		io.WriteString(&buf, "$")
		io.WriteString(&buf, k)
		io.WriteString(&buf, ":")
		writeArgumentType(&buf, reflect.TypeOf(variables[k]), true)
		// Don't insert a comma here.
		// Commas in GraphQL are insignificant, and we want minified output.
		// See https://facebook.github.io/graphql/October2016/#sec-Insignificant-Commas.
	}
	return buf.String()
}

// writeArgumentType writes a minified GraphQL type for t to w.
// value indicates whether t is a value (required) type or pointer (optional) type.
// If value is true, then "!" is written at the end of t.
func writeArgumentType(w io.Writer, t reflect.Type, value bool) {
	if t.Kind() == reflect.Ptr {
		// Pointer is an optional type, so no "!" at the end of the pointer's underlying type.
		writeArgumentType(w, t.Elem(), false)
		return
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		// List. E.g., "[Int]".
		io.WriteString(w, "[")
		writeArgumentType(w, t.Elem(), true)
		io.WriteString(w, "]")
	default:
		// Named type. E.g., "Int".
		name := t.Name()
		if name == "string" { // HACK: Workaround for https://github.com/shurcooL/githubv4/issues/12.
			name = "ID"
		}
		io.WriteString(w, name)
	}

	if value {
		// Value is a required type, so add "!" to the end.
		io.WriteString(w, "!")
	}
}

// query uses writeQuery to recursively construct
// a minified query string from the provided struct v.
//
// E.g., struct{Foo Int, BarBaz *Boolean} -> "{foo,barBaz}".
func query(v interface{}, variables map[string]interface{}) string {
	var buf bytes.Buffer
	writeQuery(&buf, reflect.TypeOf(v), false, variables)
	return buf.String()
}

// writeQuery writes a minified query for t to w.
// If inline is true, the struct fields of t are inlined into parent struct.
func writeQuery(w io.Writer, t reflect.Type, inline bool, variables map[string]interface{}) {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice:
		writeQuery(w, t.Elem(), false, variables)
	case reflect.Struct:
		// If the type implements json.Unmarshaler, it's a scalar. Don't expand it.
		if reflect.PtrTo(t).Implements(jsonUnmarshaler) {
			return
		}
		if !inline {
			io.WriteString(w, "{")
		}
		for i := 0; i < t.NumField(); i++ {
			if i != 0 {
				io.WriteString(w, ",")
			}
			f := t.Field(i)
			value, ok := f.Tag.Lookup("graphql")
			inlineField := f.Anonymous && !ok
			graphqlValue := ``
			graphqlVar := ``
			if !inlineField {
				if ok {
					graphqlValue = value
					index := strings.IndexAny(graphqlValue, `(:[$!@`)
					if index == -1 {
						graphqlVar = graphqlValue
					} else {
						graphqlVar = graphqlValue[:index]
					}
				} else {
					graphqlValue = ident.ParseMixedCaps(f.Name).ToLowerCamelCase()
					graphqlVar = value
				}
			}

			extendByKey, ifExtend := f.Tag.Lookup("graphql-extend")
			if ifExtend && extendByKey == `true` {
				fmt.Println(graphqlVar)
				times := len(variables[graphqlVar].([]map[string]interface{}))
				for i := 0; i < times; i++ {
					if i != 0 {
						io.WriteString(w, ",")
					}
					io.WriteString(w, fmt.Sprintf(`%s__%d:`, graphqlVar, i))
					if !inlineField {
						io.WriteString(w, strings.ReplaceAll(graphqlValue, `$`, fmt.Sprintf(`$%s__%d__`, graphqlVar, i)))
					}
					writeQuery(w, f.Type, inlineField, variables)
				}

			} else {
				if !inlineField {
					io.WriteString(w, graphqlValue)
				}
				writeQuery(w, f.Type, inlineField, variables)
			}

		}
		if !inline {
			io.WriteString(w, "}")
		}
	}
}

var jsonUnmarshaler = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
