package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

// PHPSerialize encodes a Go map into PHP serialized format.
// This is needed for communicating with the HoN master server at
// api.projectkongor.com which uses PHP serialization for request/response.
//
// Supported types: string, int, float64, bool, nil, map, slice.
func PHPSerialize(v interface{}) (string, error) {
	return phpSerializeValue(v)
}

// PHPUnserialize decodes a PHP serialized string into a Go map.
func PHPUnserialize(data string) (interface{}, error) {
	p := &phpParser{data: data, pos: 0}
	return p.parse()
}

func phpSerializeValue(v interface{}) (string, error) {
	if v == nil {
		return "N;", nil
	}

	switch val := v.(type) {
	case string:
		return fmt.Sprintf("s:%d:\"%s\";", len(val), val), nil

	case int:
		return fmt.Sprintf("i:%d;", val), nil

	case int64:
		return fmt.Sprintf("i:%d;", val), nil

	case float64:
		return fmt.Sprintf("d:%s;", strconv.FormatFloat(val, 'f', -1, 64)), nil

	case bool:
		if val {
			return "b:1;", nil
		}
		return "b:0;", nil

	case map[string]interface{}:
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("a:%d:{", len(val)))
		for k, v := range val {
			keyStr, _ := phpSerializeValue(k)
			valStr, err := phpSerializeValue(v)
			if err != nil {
				return "", fmt.Errorf("failed to serialize map value for key %s: %w", k, err)
			}
			sb.WriteString(keyStr)
			sb.WriteString(valStr)
		}
		sb.WriteString("}")
		return sb.String(), nil

	case []interface{}:
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("a:%d:{", len(val)))
		for i, v := range val {
			keyStr, _ := phpSerializeValue(i)
			valStr, err := phpSerializeValue(v)
			if err != nil {
				return "", fmt.Errorf("failed to serialize array index %d: %w", i, err)
			}
			sb.WriteString(keyStr)
			sb.WriteString(valStr)
		}
		sb.WriteString("}")
		return sb.String(), nil

	default:
		return "", fmt.Errorf("unsupported type for PHP serialization: %T", v)
	}
}

// phpParser is a simple recursive descent parser for PHP serialized data.
type phpParser struct {
	data string
	pos  int
}

func (p *phpParser) parse() (interface{}, error) {
	if p.pos >= len(p.data) {
		return nil, fmt.Errorf("unexpected end of data")
	}

	typ := p.data[p.pos]
	switch typ {
	case 'N':
		return p.parseNull()
	case 'b':
		return p.parseBool()
	case 'i':
		return p.parseInt()
	case 'd':
		return p.parseFloat()
	case 's':
		return p.parseString()
	case 'a':
		return p.parseArray()
	default:
		return nil, fmt.Errorf("unknown type '%c' at position %d", typ, p.pos)
	}
}

func (p *phpParser) parseNull() (interface{}, error) {
	if !p.expect("N;") {
		return nil, fmt.Errorf("expected N; at position %d", p.pos)
	}
	return nil, nil
}

func (p *phpParser) parseBool() (interface{}, error) {
	if !p.expect("b:") {
		return nil, fmt.Errorf("expected b: at position %d", p.pos)
	}
	val := p.data[p.pos]
	p.pos++
	if !p.expect(";") {
		return nil, fmt.Errorf("expected ; after bool at position %d", p.pos)
	}
	return val == '1', nil
}

func (p *phpParser) parseInt() (interface{}, error) {
	if !p.expect("i:") {
		return nil, fmt.Errorf("expected i: at position %d", p.pos)
	}
	end := strings.IndexByte(p.data[p.pos:], ';')
	if end == -1 {
		return nil, fmt.Errorf("unterminated integer at position %d", p.pos)
	}
	val, err := strconv.Atoi(p.data[p.pos : p.pos+end])
	if err != nil {
		return nil, fmt.Errorf("invalid integer at position %d: %w", p.pos, err)
	}
	p.pos += end + 1
	return val, nil
}

func (p *phpParser) parseFloat() (interface{}, error) {
	if !p.expect("d:") {
		return nil, fmt.Errorf("expected d: at position %d", p.pos)
	}
	end := strings.IndexByte(p.data[p.pos:], ';')
	if end == -1 {
		return nil, fmt.Errorf("unterminated float at position %d", p.pos)
	}
	val, err := strconv.ParseFloat(p.data[p.pos:p.pos+end], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid float at position %d: %w", p.pos, err)
	}
	p.pos += end + 1
	return val, nil
}

func (p *phpParser) parseString() (string, error) {
	if !p.expect("s:") {
		return "", fmt.Errorf("expected s: at position %d", p.pos)
	}
	// Read length
	end := strings.IndexByte(p.data[p.pos:], ':')
	if end == -1 {
		return "", fmt.Errorf("unterminated string length at position %d", p.pos)
	}
	length, err := strconv.Atoi(p.data[p.pos : p.pos+end])
	if err != nil {
		return "", fmt.Errorf("invalid string length at position %d: %w", p.pos, err)
	}
	p.pos += end + 1

	// Expect opening quote
	if p.pos >= len(p.data) || p.data[p.pos] != '"' {
		return "", fmt.Errorf("expected '\"' at position %d", p.pos)
	}
	p.pos++

	// Read string content
	if p.pos+length > len(p.data) {
		return "", fmt.Errorf("string extends beyond data at position %d", p.pos)
	}
	val := p.data[p.pos : p.pos+length]
	p.pos += length

	// Expect closing quote and semicolon
	if !p.expect("\";") {
		return "", fmt.Errorf("expected \"; at position %d", p.pos)
	}

	return val, nil
}

func (p *phpParser) parseArray() (interface{}, error) {
	if !p.expect("a:") {
		return nil, fmt.Errorf("expected a: at position %d", p.pos)
	}

	// Read count
	end := strings.IndexByte(p.data[p.pos:], ':')
	if end == -1 {
		return nil, fmt.Errorf("unterminated array count at position %d", p.pos)
	}
	count, err := strconv.Atoi(p.data[p.pos : p.pos+end])
	if err != nil {
		return nil, fmt.Errorf("invalid array count at position %d: %w", p.pos, err)
	}
	p.pos += end + 1

	if !p.expect("{") {
		return nil, fmt.Errorf("expected { at position %d", p.pos)
	}

	result := make(map[string]interface{}, count)
	for i := 0; i < count; i++ {
		// Parse key
		key, err := p.parse()
		if err != nil {
			return nil, fmt.Errorf("failed to parse array key %d: %w", i, err)
		}

		// Parse value
		val, err := p.parse()
		if err != nil {
			return nil, fmt.Errorf("failed to parse array value %d: %w", i, err)
		}

		// Convert key to string
		switch k := key.(type) {
		case string:
			result[k] = val
		case int:
			result[strconv.Itoa(k)] = val
		default:
			result[fmt.Sprintf("%v", k)] = val
		}
	}

	if !p.expect("}") {
		return nil, fmt.Errorf("expected } at position %d", p.pos)
	}

	return result, nil
}

func (p *phpParser) expect(s string) bool {
	if p.pos+len(s) > len(p.data) {
		return false
	}
	if p.data[p.pos:p.pos+len(s)] == s {
		p.pos += len(s)
		return true
	}
	return false
}
