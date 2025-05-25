package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

// It's good practice to register your types with gob, especially if they are interfaces
// or used within interfaces. For concrete structs sent directly, it's often not strictly
// necessary but doesn't hurt.

func init() {
	gob.Register(MyCommand{})
	gob.Register(map[string]int{})
}

func sendCommand(conn net.Conn, cmd MyCommand) error {
	// 1. Serialize the command using gob
	var serializedCmd bytes.Buffer
	encoder := gob.NewEncoder(&serializedCmd)
	if err := encoder.Encode(cmd); err != nil {
		return fmt.Errorf("failed to gob encode command: %w", err)
	}

	// 2. Get the length of the serialized command
	cmdBytes := serializedCmd.Bytes()
	cmdLength := int32(len(cmdBytes)) // Using int32 for length, adjust as needed

	// 3. Create the length prefix (e.g., 4 bytes for int32)
	lengthPrefix := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthPrefix, uint32(cmdLength)) // Or LittleEndian, be consistent!

	// 4. Send the length prefix
	_, err := conn.Write(lengthPrefix)
	if err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}

	// 5. Send the actual command
	_, err = conn.Write(cmdBytes)
	if err != nil {
		return fmt.Errorf("failed to write command: %w", err)
	}

	log.Printf("Sent command: %+v (length: %d bytes)", cmd, cmdLength)
	return nil
}

func main() {
	conn, err := net.Dial("tcp", "localhost:8000")
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	f, err := os.Open("./bigdata.dat")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		log.Fatalf("Failed to get file size: %v", err)
	}
	buffSize := make([]byte, 8)
	binary.BigEndian.PutUint64(buffSize, uint64(fi.Size()))

	_, err = conn.Write(buffSize)
	if err != nil {
		log.Fatalf("Error writing file size: %v", err)
	}

	reader := bufio.NewReader(f)
	buff := make([]byte, 4096)
	for {
		n, err := reader.Read(buff)
		if err != nil {
			if io.EOF == err || io.ErrUnexpectedEOF == err {
				return
			}
		}
		if n > 0 {
			_, err = conn.Write(buff[:n]) // Note: using buff[:n] to write only what was read
			if err != nil {
				switch err := err.(type) {
				case *net.OpError:
					// Handles network operation errors like connection refused, timeout
					log.Fatalf("Network operation error: %v", err)
					return
				default:
					// Handle other errors like:
					// - Connection reset by peer
					// - Broken pipe
					// - Connection timeout
					log.Fatalf("Write error: %v", err)
				}
			}
		}
	}
	log.Println("Done transfering file")
}
