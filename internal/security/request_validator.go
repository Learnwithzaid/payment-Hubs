package security

import (
    "bytes"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "strings"

    "github.com/santhosh-tekuri/jsonschema/v5"
)

type JSONSchemaValidator struct {
    schema *jsonschema.Schema
}

func NewJSONSchemaValidator(schemaJSON string) (*JSONSchemaValidator, error) {
    compiler := jsonschema.NewCompiler()
    if err := compiler.AddResource("schema.json", strings.NewReader(schemaJSON)); err != nil {
        return nil, err
    }
    schema, err := compiler.Compile("schema.json")
    if err != nil {
        return nil, err
    }

    return &JSONSchemaValidator{schema: schema}, nil
}

func (v *JSONSchemaValidator) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Body == nil {
            WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        body, err := io.ReadAll(r.Body)
        if err != nil {
            var mbe *http.MaxBytesError
            if errors.As(err, &mbe) {
                WriteJSONError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large")
                return
            }
            WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }
        _ = r.Body.Close()
        r.Body = io.NopCloser(bytes.NewReader(body))

        var payload interface{}
        dec := json.NewDecoder(bytes.NewReader(body))
        dec.UseNumber()
        if err := dec.Decode(&payload); err != nil {
            WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }

        if err := v.schema.Validate(payload); err != nil {
            WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        r.Body = io.NopCloser(bytes.NewReader(body))
        next.ServeHTTP(w, r)
    })
}
