package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
)

const (
	DefaultMaxArgumentsBytes = 64 << 10
	DefaultMaxOutputBytes    = 1 << 20
	DefaultMaxJSONDepth      = 16
	maxSchemaBytes           = 64 << 10
)

var supportedSchemaKeywords = map[string]struct{}{
	"type": {}, "properties": {}, "required": {}, "additionalProperties": {},
	"items": {}, "enum": {}, "minLength": {}, "maxLength": {},
	"minimum": {}, "maximum": {}, "minItems": {}, "maxItems": {},
	"title": {}, "description": {}, "default": {}, "examples": {},
}

func ValidateSchemaDefinition(schema json.RawMessage) error {
	if len(schema) == 0 || len(schema) > maxSchemaBytes {
		return fmt.Errorf("%w: schema is empty or too large", ErrInvalidTool)
	}
	value, err := decodeJSON(schema)
	if err != nil {
		return fmt.Errorf("%w: invalid JSON schema", ErrInvalidTool)
	}
	object, ok := value.(map[string]any)
	if !ok || object["type"] != "object" {
		return fmt.Errorf("%w: schema must be an object with type", ErrInvalidTool)
	}
	if jsonDepth(value) > DefaultMaxJSONDepth {
		return fmt.Errorf("%w: schema exceeds maximum depth", ErrInvalidTool)
	}
	if err := validateSchemaNode(object, "$", 0); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTool, err)
	}
	return nil
}

func validateSchemaNode(definition map[string]any, path string, depth int) error {
	if depth > DefaultMaxJSONDepth {
		return errors.New("schema recursion limit exceeded")
	}
	for keyword := range definition {
		if _, supported := supportedSchemaKeywords[keyword]; !supported {
			return fmt.Errorf("%s uses unsupported keyword %q", path, keyword)
		}
	}
	typeName, ok := definition["type"].(string)
	if !ok {
		return fmt.Errorf("%s type is required", path)
	}
	if enumValue, exists := definition["enum"]; exists {
		enum, ok := enumValue.([]any)
		if !ok || len(enum) == 0 {
			return fmt.Errorf("%s enum must be a non-empty array", path)
		}
	}
	switch typeName {
	case "object":
		properties, exists := definition["properties"]
		propertyMap := map[string]any{}
		if exists {
			propertyMap, ok = properties.(map[string]any)
			if !ok {
				return fmt.Errorf("%s properties must be an object", path)
			}
			for name, child := range propertyMap {
				childDefinition, ok := child.(map[string]any)
				if !ok {
					return fmt.Errorf("%s.%s must be a schema", path, name)
				}
				if err := validateSchemaNode(childDefinition, path+"."+name, depth+1); err != nil {
					return err
				}
			}
		}
		if requiredValue, exists := definition["required"]; exists {
			required, ok := requiredValue.([]any)
			if !ok {
				return fmt.Errorf("%s required must be an array", path)
			}
			for _, item := range required {
				name, ok := item.(string)
				if !ok || name == "" {
					return fmt.Errorf("%s required entries must be non-empty strings", path)
				}
				if _, exists := propertyMap[name]; !exists {
					return fmt.Errorf("%s required property %q is not declared", path, name)
				}
			}
		}
		additionalValue, exists := definition["additionalProperties"]
		if !exists {
			return fmt.Errorf("%s must declare additionalProperties", path)
		}
		switch typed := additionalValue.(type) {
		case bool:
			if typed {
				return fmt.Errorf("%s cannot allow arbitrary properties", path)
			}
		case map[string]any:
			if err := validateSchemaNode(typed, path+".*", depth+1); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s additionalProperties is invalid", path)
		}
	case "array":
		items, ok := definition["items"].(map[string]any)
		if !ok {
			return fmt.Errorf("%s items schema is required", path)
		}
		if err := validateNonNegativeIntegerKeywords(definition, path, "minItems", "maxItems"); err != nil {
			return err
		}
		return validateSchemaNode(items, path+"[]", depth+1)
	case "string":
		if err := validateNonNegativeIntegerKeywords(definition, path, "minLength", "maxLength"); err != nil {
			return err
		}
	case "integer", "number":
		for _, keyword := range []string{"minimum", "maximum"} {
			if value, exists := definition[keyword]; exists {
				if _, ok := schemaNumber(value); !ok {
					return fmt.Errorf("%s %s must be a number", path, keyword)
				}
			}
		}
	case "boolean", "null":
	default:
		return fmt.Errorf("%s uses unsupported type %q", path, typeName)
	}
	return nil
}

func validateNonNegativeIntegerKeywords(definition map[string]any, path string, keywords ...string) error {
	for _, keyword := range keywords {
		if value, exists := definition[keyword]; exists {
			integer, ok := schemaInt(value)
			if !ok || integer < 0 {
				return fmt.Errorf("%s %s must be a non-negative integer", path, keyword)
			}
		}
	}
	return nil
}

func ValidateJSON(raw json.RawMessage, schema json.RawMessage, maxBytes, maxDepth int, result bool) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxArgumentsBytes
	}
	if maxDepth <= 0 {
		maxDepth = DefaultMaxJSONDepth
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	code := CodeInvalidArguments
	if result {
		code = CodeInvalidResult
	}
	if len(raw) > maxBytes {
		return NewError(code, "JSON payload exceeds size limit")
	}
	value, err := decodeJSON(raw)
	if err != nil {
		return WrapError(code, "JSON payload is invalid", err)
	}
	if jsonDepth(value) > maxDepth {
		return NewError(code, "JSON payload exceeds depth limit")
	}
	schemaValue, err := decodeJSON(schema)
	if err != nil {
		return WrapError(CodeInternal, "tool schema is invalid", err)
	}
	if err := validateSchemaValue(value, schemaValue, "$", 0, maxDepth); err != nil {
		var agentErr *Error
		if errors.As(err, &agentErr) {
			agentErr.Code = code
			return agentErr
		}
		return WrapError(code, "JSON payload does not match schema", err)
	}
	return nil
}

func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple JSON values")
	}
	return value, nil
}

func jsonDepth(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		depth := 1
		for _, child := range typed {
			depth = max(depth, 1+jsonDepth(child))
		}
		return depth
	case []any:
		depth := 1
		for _, child := range typed {
			depth = max(depth, 1+jsonDepth(child))
		}
		return depth
	default:
		return 1
	}
}

func validateSchemaValue(value, schema any, path string, depth, maxDepth int) error {
	if depth > maxDepth {
		return invalidField(path, "schema recursion limit exceeded")
	}
	definition, ok := schema.(map[string]any)
	if !ok {
		return invalidField(path, "invalid schema definition")
	}
	if enumValues, ok := definition["enum"].([]any); ok {
		matched := false
		for _, candidate := range enumValues {
			if reflect.DeepEqual(value, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return invalidField(path, "value is not in the allowed set")
		}
	}
	typeName, _ := definition["type"].(string)
	switch typeName {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return invalidField(path, "must be an object")
		}
		properties, _ := definition["properties"].(map[string]any)
		if required, ok := definition["required"].([]any); ok {
			for _, item := range required {
				name, _ := item.(string)
				if _, exists := object[name]; name != "" && !exists {
					return invalidField(path+"."+name, "is required")
				}
			}
		}
		additional, hasAdditional := definition["additionalProperties"].(bool)
		additionalSchema, hasAdditionalSchema := definition["additionalProperties"].(map[string]any)
		for name, child := range object {
			childSchema, exists := properties[name]
			if !exists {
				if hasAdditional && !additional {
					return invalidField(path+"."+name, "is not allowed")
				}
				if hasAdditionalSchema {
					if err := validateSchemaValue(child, additionalSchema, path+"."+name, depth+1, maxDepth); err != nil {
						return err
					}
				}
				continue
			}
			if err := validateSchemaValue(child, childSchema, path+"."+name, depth+1, maxDepth); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return invalidField(path, "must be an array")
		}
		if maximum, ok := schemaInt(definition["maxItems"]); ok && len(items) > maximum {
			return invalidField(path, "contains too many items")
		}
		if minimum, ok := schemaInt(definition["minItems"]); ok && len(items) < minimum {
			return invalidField(path, "contains too few items")
		}
		if itemSchema, exists := definition["items"]; exists {
			for index, item := range items {
				if err := validateSchemaValue(item, itemSchema, fmt.Sprintf("%s[%d]", path, index), depth+1, maxDepth); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return invalidField(path, "must be a string")
		}
		if maximum, ok := schemaInt(definition["maxLength"]); ok && len([]rune(text)) > maximum {
			return invalidField(path, "is too long")
		}
		if minimum, ok := schemaInt(definition["minLength"]); ok && len([]rune(text)) < minimum {
			return invalidField(path, "is too short")
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || strings.Contains(number.String(), ".") {
			return invalidField(path, "must be an integer")
		}
		integer, err := number.Int64()
		if err != nil {
			return invalidField(path, "must be an integer")
		}
		if minimum, ok := schemaNumber(definition["minimum"]); ok && float64(integer) < minimum {
			return invalidField(path, "is below minimum")
		}
		if maximum, ok := schemaNumber(definition["maximum"]); ok && float64(integer) > maximum {
			return invalidField(path, "exceeds maximum")
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return invalidField(path, "must be a number")
		}
		parsed, err := number.Float64()
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return invalidField(path, "must be a finite number")
		}
		if minimum, ok := schemaNumber(definition["minimum"]); ok && parsed < minimum {
			return invalidField(path, "is below minimum")
		}
		if maximum, ok := schemaNumber(definition["maximum"]); ok && parsed > maximum {
			return invalidField(path, "exceeds maximum")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return invalidField(path, "must be a boolean")
		}
	case "null":
		if value != nil {
			return invalidField(path, "must be null")
		}
	default:
		return invalidField(path, "schema type is unsupported")
	}
	return nil
}

func schemaInt(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := number.Int64()
	return int(integer), err == nil
}

func schemaNumber(value any) (float64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Float64()
	return parsed, err == nil
}
