package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"log"
	"net"
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

	command := MyCommand{Action: "CREATE_USER", Payload: "Alice"}
	if err := sendCommand(conn, command); err != nil {
		log.Printf("Error sending command: %v", err)
	}

	command2 := MyCommand{Action: "GET_DATA", Payload: map[string]int{"id": 123}}
	if err := sendCommand(conn, command2); err != nil {
		log.Printf("Error sending command: %v", err)
	}
	// ... send more commands
}
