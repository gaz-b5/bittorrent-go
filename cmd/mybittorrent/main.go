package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"

	bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

type Peer struct {
	IP   net.IP
	Port uint16
}

type RequestMessage struct {
	lengthPrefix uint32
	id           uint8
	index        uint32
	begin        uint32
	length       uint32
}

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

func peersList(torrentFile string) (peers []Peer, err error) {
	data, err := os.ReadFile(torrentFile)

	if err != nil {
		return peers, err
	}

	decoded, _, err := decodeDict(string(data), 0)

	if err != nil {
		return peers, err
	}

	baseURL, ok := decoded["announce"].(string)
	if !ok {
		return peers, err
	}

	info, ok := decoded["info"].(map[string]interface{})

	if !ok {
		return peers, err
	}

	var buf bytes.Buffer

	err = bencode.Marshal(&buf, info)

	if err != nil {
		return peers, err
	}

	hash := sha1.New()
	hash.Write(buf.Bytes())
	sha1Hash := hash.Sum(nil)

	u, err := url.Parse(baseURL)

	params := url.Values{}
	params.Add("info_hash", string(sha1Hash))
	params.Add("peer_id", "00112233445566778899")
	params.Add("port", "6881")
	params.Add("uploaded", "0")
	params.Add("downloaded", "0")
	params.Add("left", strconv.Itoa(info["length"].(int)))
	params.Add("compact", "1")

	u.RawQuery = params.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return peers, err
	}
	defer resp.Body.Close()

	resBody, err := io.ReadAll(resp.Body)

	decodedResp, _, err := decodeDict(string(resBody), 0)
	if err != nil {
		return peers, err
	}

	peersData := []byte(decodedResp["peers"].(string))

	if len(peersData)%6 != 0 {
		fmt.Println("invalid peersData length")
		return peers, err
	}

	for i := 0; i < len(peersData); i += 6 {
		peer := peersData[i : i+6]

		ip := net.IPv4(peer[0], peer[1], peer[2], peer[3])

		port := binary.BigEndian.Uint16(peer[4:6])

		var p Peer
		p.IP = ip
		p.Port = port

		peers = append(peers, p)
	}

	return peers, err
}

func executeHandshake(torrentFile string, peerAddress string, sha1Hash []byte, conn net.Conn) (recievedHandshake []byte, err error) {

	pstrlen := byte(19)
	pstr := []byte("BitTorrent protocol")
	reserved := make([]byte, 8)
	handshake := append([]byte{pstrlen}, pstr...)
	handshake = append(handshake, reserved...)
	handshake = append(handshake, sha1Hash...)
	handshake = append(handshake, []byte{0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9}...)

	_, err = conn.Write(handshake)
	if err != nil {
		fmt.Println("Failed to write handshake:", err)
		return recievedHandshake, err
	}

	recievedHandshake = make([]byte, 68)

	_, err = conn.Read(recievedHandshake)

	if err != nil {
		fmt.Println("Failed to read handshake:", err)
		return recievedHandshake, err
	}
	return recievedHandshake, err
}

func downloadTorrent(conn net.Conn, torrentFilePath string, index int) (pieceData []byte, err error) {

	//wait for bitfield message
	buf := make([]byte, 4)
	_, err = conn.Read(buf)
	if err != nil {
		fmt.Println(err)
		return
	}

	//payload
	bitpayload := make([]byte, binary.BigEndian.Uint32(buf))
	_, err = conn.Read(bitpayload)
	if err != nil {
		fmt.Println(err)
		return
	}

	//constructed interested
	message := make([]byte, 5)
	message[4] = byte(2)
	binary.BigEndian.PutUint32(message[0:4], uint32(1))

	//send interested
	_, err = conn.Write(message)
	if err != nil {
		fmt.Println(err)
		return
	}

	//wait for unchoke
	buf = make([]byte, 5)
	_, err = conn.Read(buf)
	if err != nil {
		fmt.Println(err)
		return
	}

	torrentFile, err := os.ReadFile(torrentFilePath)

	if err != nil {
		fmt.Printf("error: read file: %v\n", err)
		return
	}

	decoded, _, err := decodeDict(string(torrentFile), 0)

	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("Tracker URL:", decoded["announce"])

	info, _ := decoded["info"].(map[string]interface{})

	//request for each block

	pieceSize := info["piece length"].(int)
	pieceCnt := int(math.Ceil(float64(info["length"].(int)) / float64(pieceSize)))
	if index == pieceCnt-1 {
		pieceSize = info["length"].(int) % info["piece length"].(int)
	}
	blockSize := 16 * 1024
	blockCnt := int(math.Ceil(float64(pieceSize) / float64(blockSize)))
	for i := 0; i < blockCnt; i++ {
		blockLength := blockSize
		if i == blockCnt-1 {
			blockLength = pieceSize - ((blockCnt - 1) * int(blockSize))
		}

		peerMessage := RequestMessage{
			lengthPrefix: 13,
			id:           6,
			index:        uint32(index),
			begin:        uint32(i * int(blockSize)),
			length:       uint32(blockLength),
		}
		var buf bytes.Buffer
		binary.Write(&buf, binary.BigEndian, peerMessage)
		_, err = conn.Write(buf.Bytes())
		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		//accept data
		resBuf := make([]byte, 4)
		_, err = conn.Read(resBuf)
		if err != nil {
			fmt.Println(err)
			return nil, err
		}
		peerMessage = RequestMessage{}
		peerMessage.lengthPrefix = binary.BigEndian.Uint32(resBuf)
		payloadBuf := make([]byte, peerMessage.lengthPrefix)
		_, err = io.ReadFull(conn, payloadBuf)
		if err != nil {
			fmt.Println(err)
			return nil, err
		}
		peerMessage.id = payloadBuf[0]
		pieceData = append(pieceData, payloadBuf[9:]...)
	}

	return pieceData, err
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

		fmt.Println("Piece Length:", info["piece length"])

		fmt.Printf("Piece Hashes: %x\n", info["pieces"])

	} else if command == "peers" {
		torrentFile := os.Args[2]

		peers, err := peersList(torrentFile)

		if err != nil {
			fmt.Println("Error forming peer list:", err)
			return
		}

		for _, peer := range peers {
			fmt.Printf("%s:%d\n", peer.IP, peer.Port)
		}

	} else if command == "handshake" {
		torrentFile := os.Args[2]

		peerAddress := os.Args[3]

		data, err := os.ReadFile(torrentFile)

		if err != nil {
			fmt.Printf("error: read file: %v\n", err)
			return
		}

		decoded, _, err := decodeDict(string(data), 0)

		if err != nil {
			fmt.Println(err)
			return
		}

		info, ok := decoded["info"].(map[string]interface{})

		if !ok {
			fmt.Println("info is not a map")
			return
		}

		var buf bytes.Buffer

		err = bencode.Marshal(&buf, info)

		if err != nil {
			fmt.Println("Bad info")
			return
		}

		hash := sha1.New()
		hash.Write(buf.Bytes())
		sha1Hash := hash.Sum(nil)

		conn, err := net.Dial("tcp", peerAddress)
		if err != nil {
			fmt.Println("bad peer")
			return
		}
		defer conn.Close()

		recievedHandshake, err := executeHandshake(torrentFile, peerAddress, sha1Hash, conn)

		if err != nil {
			fmt.Println("Handshake error:", err)
			return
		}

		fmt.Printf("Peer ID: %x\n", recievedHandshake[48:])

	} else if command == "download_piece" {

		var torrentFile, outputPath string

		if os.Args[2] == "-o" {
			torrentFile = os.Args[4]
			outputPath = os.Args[3]
		}

		peers, err := peersList(torrentFile)
		if err != nil {
			fmt.Println(err)
			return
		}

		peer0 := fmt.Sprintf("%s:%d\n", peers[0].IP, peers[0].Port)

		index, _ := strconv.Atoi(os.Args[5])

		data, err := os.ReadFile(torrentFile)

		if err != nil {
			fmt.Printf("error: read file: %v\n", err)
			return
		}

		decoded, _, err := decodeDict(string(data), 0)

		if err != nil {
			fmt.Println(err)
			return
		}

		info, ok := decoded["info"].(map[string]interface{})

		if !ok {
			fmt.Println("info is not a map")
			return
		}

		var buf bytes.Buffer

		err = bencode.Marshal(&buf, info)

		if err != nil {
			fmt.Println("Bad info")
			return
		}

		hash := sha1.New()
		hash.Write(buf.Bytes())
		sha1Hash := hash.Sum(nil)

		conn, err := net.Dial("tcp", peer0)
		if err != nil {
			fmt.Println("bad peer")
			return
		}
		defer conn.Close()

		_, err = executeHandshake(torrentFile, peer0, sha1Hash, conn)

		if err != nil {
			fmt.Println("Handshake error:", err)
			return
		}

		pieceData, err := downloadTorrent(conn, torrentFile, index)
		if err != nil {
			fmt.Println(err)
			return
		}

		file, err := os.Create(outputPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer file.Close()

		_, err = file.Write(pieceData)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Printf("Piece %d downloaded to %s.\n", index, outputPath)

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
