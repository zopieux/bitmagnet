// Minimal bencode encoder/decoder for the fake DHT/peer.
package main

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
)

func bencode(v interface{}) []byte {
	switch t := v.(type) {
	case string:
		return []byte(strconv.Itoa(len(t)) + ":" + t)
	case []byte:
		var buf bytes.Buffer
		buf.WriteString(strconv.Itoa(len(t)))
		buf.WriteByte(':')
		buf.Write(t)
		return buf.Bytes()
	case int:
		return []byte("i" + strconv.Itoa(t) + "e")
	case int64:
		return []byte("i" + strconv.FormatInt(t, 10) + "e")
	case []interface{}:
		var buf bytes.Buffer
		buf.WriteByte('l')
		for _, e := range t {
			buf.Write(bencode(e))
		}
		buf.WriteByte('e')
		return buf.Bytes()
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('d')
		for _, k := range keys {
			buf.Write(bencode(k))
			buf.Write(bencode(t[k]))
		}
		buf.WriteByte('e')
		return buf.Bytes()
	default:
		panic(fmt.Sprintf("bencode: unsupported type %T", v))
	}
}

type decoder struct {
	data []byte
	pos  int
}

func bdecode(data []byte) (interface{}, error) {
	d := &decoder{data: data}
	v, err := d.value()
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (d *decoder) value() (interface{}, error) {
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("bdecode: unexpected end")
	}
	c := d.data[d.pos]
	switch {
	case c == 'i':
		return d.integer()
	case c == 'l':
		return d.list()
	case c == 'd':
		return d.dict()
	case c >= '0' && c <= '9':
		return d.bytes()
	default:
		return nil, fmt.Errorf("bdecode: unexpected byte %q", c)
	}
}

func (d *decoder) integer() (int64, error) {
	d.pos++ // 'i'
	end := bytes.IndexByte(d.data[d.pos:], 'e')
	if end < 0 {
		return 0, fmt.Errorf("bdecode: unterminated integer")
	}
	n, err := strconv.ParseInt(string(d.data[d.pos:d.pos+end]), 10, 64)
	if err != nil {
		return 0, err
	}
	d.pos += end + 1
	return n, nil
}

func (d *decoder) bytes() ([]byte, error) {
	colon := bytes.IndexByte(d.data[d.pos:], ':')
	if colon < 0 {
		return nil, fmt.Errorf("bdecode: no colon in string")
	}
	n, err := strconv.Atoi(string(d.data[d.pos : d.pos+colon]))
	if err != nil {
		return nil, err
	}
	d.pos += colon + 1
	if d.pos+n > len(d.data) {
		return nil, fmt.Errorf("bdecode: string overruns buffer")
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

func (d *decoder) list() ([]interface{}, error) {
	d.pos++ // 'l'
	var out []interface{}
	for {
		if d.pos >= len(d.data) {
			return nil, fmt.Errorf("bdecode: unterminated list")
		}
		if d.data[d.pos] == 'e' {
			d.pos++
			return out, nil
		}
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

func (d *decoder) dict() (map[string]interface{}, error) {
	d.pos++ // 'd'
	out := make(map[string]interface{})
	for {
		if d.pos >= len(d.data) {
			return nil, fmt.Errorf("bdecode: unterminated dict")
		}
		if d.data[d.pos] == 'e' {
			d.pos++
			return out, nil
		}
		kb, err := d.bytes()
		if err != nil {
			return nil, err
		}
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		out[string(kb)] = v
	}
}
