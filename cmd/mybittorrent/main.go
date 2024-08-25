package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"os"

	bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345
func decode(b string, st int) (x interface{}, i int, err error) {
	// fmt.Println(st)
	if st == len(b) {
		return nil, st, io.ErrUnexpectedEOF
	}
	i = st
	switch {
	case b[i] == 'l':
		return decodeList(b, i)
	case b[i] == 'i':
		return decodeInt(b, i)
	case b[i] >= '0' && b[i] <= '9':
		return decodeString(b, i)
	case b[i] == 'd':
		return decodeDict(b, i)
	default:
		return nil, st, fmt.Errorf("unexpected value: %q", b[i])
	}
}

func decodeString(b string, st int) (x string, i int, err error) {
	var l int
	i = st
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		l = l*10 + (int(b[i]) - '0')
		i++
	}
	if i == len(b) || b[i] != ':' {
		return "", st, fmt.Errorf("bad string")
	}
	i++
	if i+l > len(b) {
		return "", st, fmt.Errorf("bad string: out of bounds")
	}
	x = b[i : i+l]
	i += l
	return x, i, nil
}

func decodeInt(b string, st int) (x int, i int, err error) {
	i = st
	i++ // 'i'
	if i == len(b) {
		return 0, st, fmt.Errorf("bad int")
	}
	neg := false
	if b[i] == '-' {
		neg = true
		i++
	}
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		x = x*10 + (int(b[i]) - '0')
		i++
	}
	if i == len(b) || b[i] != 'e' {
		return 0, st, fmt.Errorf("bad int")
	}
	i++
	if neg {
		x = -x
	}
	return x, i, nil
}
func decodeList(b string, st int) (l []interface{}, i int, err error) {
	i = st
	i++ // 'l'
	l = make([]interface{}, 0)
	for {
		if i >= len(b) {
			return nil, st, fmt.Errorf("bad list")
		}
		if b[i] == 'e' {
			break
		}
		var x interface{}
		x, i, err = decode(b, i)
		if err != nil {
			return nil, i, err
		}
		l = append(l, x)
	}
	i++
	return l, i, nil
}

func decodeDict(b string, st int) (m map[string]interface{}, i int, err error) {
	i = st
	i++
	m = make(map[string]interface{})
	for {
		if i >= len(b) {
			return nil, st, fmt.Errorf("bad dictionary")
		}
		if b[i] == 'e' {
			break
		}
		var key string
		key, i, err = decodeString(b, i)
		if err != nil {
			return nil, i, err
		}
		var value interface{}
		value, i, err = decode(b, i)
		if err != nil {
			return nil, i, err
		}
		m[key] = value
	}
	return m, i, nil
}

func main() {

	command := os.Args[1]

	if command == "decode" {

		bencodedValue := os.Args[2]

		decoded, _, err := decode(bencodedValue, 0)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))

	} else if command == "info" {
		data, err := os.ReadFile(os.Args[2])

		if err != nil {
			fmt.Printf("error: read file: %v\n", err)
			return
		}

		decoded, _, err := decodeDict(string(data), 0)

		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println("Tracker URL:", decoded["announce"])

		info, ok := decoded["info"].(map[string]interface{})

		if !ok {
			fmt.Println("info is not a map")
			return
		}
		fmt.Println("Length:", info["length"])

		var buf bytes.Buffer

		err = bencode.Marshal(&buf, info)

		if err != nil {
			fmt.Println("Bad info")
			return
		}

		hash := sha1.New()
		hash.Write(buf.Bytes())
		sha1Hash := hash.Sum(nil)

		fmt.Printf("Info Hash: %x\n", sha1Hash)

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
