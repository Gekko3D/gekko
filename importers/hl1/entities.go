package hl1

import (
	"fmt"
	"strconv"
	"strings"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

type KeyValue struct {
	Key   string
	Value string
}

type RawEntity struct {
	Pairs []KeyValue
}

func (e RawEntity) Value(key string) string {
	for i := len(e.Pairs) - 1; i >= 0; i-- {
		if strings.EqualFold(e.Pairs[i].Key, key) {
			return e.Pairs[i].Value
		}
	}
	return ""
}

func (e RawEntity) ClassName() string {
	return e.Value("classname")
}

func (e RawEntity) Map() map[string]string {
	out := make(map[string]string, len(e.Pairs))
	for _, pair := range e.Pairs {
		out[pair.Key] = pair.Value
	}
	return out
}

func ParseEntities(text string) ([]RawEntity, error) {
	parser := entityParser{text: text}
	var entities []RawEntity
	for {
		parser.skipSpace()
		if parser.eof() {
			return entities, nil
		}
		if !parser.consume('{') {
			return nil, parser.errf("expected entity start")
		}
		var entity RawEntity
		for {
			parser.skipSpace()
			if parser.consume('}') {
				break
			}
			key, err := parser.quoted()
			if err != nil {
				return nil, err
			}
			parser.skipSpace()
			value, err := parser.quoted()
			if err != nil {
				return nil, err
			}
			entity.Pairs = append(entity.Pairs, KeyValue{Key: key, Value: value})
		}
		entities = append(entities, entity)
	}
}

type entityParser struct {
	text string
	pos  int
}

func (p *entityParser) eof() bool {
	return p.pos >= len(p.text)
}

func (p *entityParser) skipSpace() {
	for !p.eof() {
		switch p.text[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

func (p *entityParser) consume(ch byte) bool {
	if p.eof() || p.text[p.pos] != ch {
		return false
	}
	p.pos++
	return true
}

func (p *entityParser) quoted() (string, error) {
	if !p.consume('"') {
		return "", p.errf("expected quoted string")
	}
	var b strings.Builder
	for !p.eof() {
		ch := p.text[p.pos]
		p.pos++
		if ch == '"' {
			return b.String(), nil
		}
		if ch == '\\' && !p.eof() && (p.text[p.pos] == '"' || p.text[p.pos] == '\\') {
			next := p.text[p.pos]
			p.pos++
			b.WriteByte(next)
			continue
		}
		b.WriteByte(ch)
	}
	return "", p.errf("unterminated quoted string")
}

func (p *entityParser) errf(format string, args ...any) error {
	return fmt.Errorf("entity parse byte %d: %s", p.pos, fmt.Sprintf(format, args...))
}

func parseVec3(value string) (importcommon.Vec3, bool) {
	fields := strings.Fields(value)
	if len(fields) != 3 {
		return importcommon.Vec3{}, false
	}
	x, errX := strconv.ParseFloat(fields[0], 32)
	y, errY := strconv.ParseFloat(fields[1], 32)
	z, errZ := strconv.ParseFloat(fields[2], 32)
	if errX != nil || errY != nil || errZ != nil {
		return importcommon.Vec3{}, false
	}
	return importcommon.Vec3{X: float32(x), Y: float32(y), Z: float32(z)}, true
}

func parseFloat32(value string) (float32, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 32)
	if err != nil {
		return 0, false
	}
	return float32(parsed), true
}
