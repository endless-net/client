package transport_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type closedLoader struct{}

func (closedLoader) Load(uri string) (any, error) {
	return nil, fmt.Errorf("schema is not registered locally: %s", uri)
}

func TestTransportSchema(t *testing.T) {
	const schemaPath = "schemas/transport.schema.json"
	const schemaID = "urn:endlessnet:client:transport:v1"
	file, err := os.Open(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read schema: %v %v", err, closeErr)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	compiler.UseLoader(closedLoader{})
	if err := compiler.AddResource(schemaID, document); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(schemaID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("fixtures/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Schema, Instance string
		Valid                  *bool
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty transport fixture manifest")
	}
	for _, c := range fixtures {
		t.Run(c.Name, func(t *testing.T) {
			source, fragment, _ := strings.Cut(c.Schema, "#")
			if source != schemaPath || !fs.ValidPath(c.Instance) || !strings.HasPrefix(c.Instance, "fixtures/") || c.Valid == nil {
				t.Fatal("invalid transport fixture")
			}
			schema, err := compiler.Compile(schemaID + "#" + fragment)
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(c.Instance)
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonschema.UnmarshalJSON(file)
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("read fixture: %v %v", err, closeErr)
			}
			if err := schema.Validate(value); (err == nil) != *c.Valid {
				t.Fatalf("valid=%v: %v", *c.Valid, err)
			}
		})
	}
}
