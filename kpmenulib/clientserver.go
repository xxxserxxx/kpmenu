package kpmenulib

import (
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Packet is the data sent by the client to the server listener
type Packet struct {
	CliArguments []string
}

// StartClient sends a packet to the server listener
func StartClient() error {
	port, err := getPort()
	if err != nil {
		return err
	}

	conn, err := net.Dial("tcp", "localhost:"+port)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send the packet
	enc := gob.NewEncoder(conn)
	err = enc.Encode(Packet{CliArguments: os.Args[1:]})
	return err
}

// StartServer starts to listen for client packets
func StartServer(m *Menu) (err error) {
	if m.Configuration.Flags.Daemon {
		log.Printf("Executing as daemon")
	}

	if m.Configuration.General.NoCache && !m.Configuration.Flags.Daemon {
		// Directly execute kpmenu
		if fatal := m.Execute(); fatal == true {
			os.Exit(1) // Set exit code to 1 and exit
		}
	} else {
		// Handle packet request
		handlePacket := func(packet Packet) bool {
			log.Printf("received a client call with args \"%v\"", packet.CliArguments)
			m.Configuration.Flags.Autotype = false
			m.CliArguments = packet.CliArguments
			return m.Show()
		}

		// Execute kpmenu for the first time, if not a daemon
		exit := false
		if !m.Configuration.Flags.Daemon {
			exit = m.Execute()
		}

		// If exit is false (cache on) listen for client calls
		if !exit {
			err = setupListener(m, handlePacket)
		}
	}
	return
}

func setupListener(m *Menu, handlePacket func(Packet) bool) error {
	// Listen for client calls
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}
	tcpListener := listener.(*net.TCPListener)
	defer tcpListener.Close()

	// Get used port
	_, port, _ := net.SplitHostPort(listener.Addr().String())

	// Save port
	if err := savePort(port); err != nil {
		return err
	}

	exit := false
	for !exit {
		if !m.Configuration.Flags.Daemon {
			// If not a daemon prepare cache time
			remainingCacheTime := m.Configuration.General.CacheTimeout - time.Now().Sub(m.CacheStart)
			tcpListener.SetDeadline(time.Now().Add(remainingCacheTime))
		}

		// Listen to calls
		conn, err := listener.Accept()
		if err != nil {
			netErr := err.(*net.OpError)
			if netErr.Timeout() {
				log.Print("cache timed out")
				return nil
			}
			return err
		}
		defer conn.Close()

		// Go routine to handle input
		ch := make(chan Packet)
		errCh := make(chan error)
		go func(ch chan Packet, errCh chan error) {
			dec := gob.NewDecoder(conn)
			var packet Packet
			err := dec.Decode(&packet)
			if err != nil {
				if err != io.EOF {
					errCh <- err
				} else {
					return
				}
			}
			ch <- packet
		}(ch, errCh)

		// Handle received input
		timeout := time.Tick(3 * time.Second) // Timeout of 3 seconds - to avoid problems
		select {
		case packet := <-ch:
			// Received the data
			fatal := handlePacket(packet)
			exit = (fatal && !m.Configuration.Flags.Daemon)
			break
		case err := <-errCh:
			// Received an error
			return err
		case <-timeout:
			// Timed out
			log.Printf("received request is timed out")
		}
	}

	return nil
}

func makeCacheFolder() error {
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".cache/kpmenu/"), 0755); err != nil {
		return fmt.Errorf("failed to make cache folder: %v", err)
	}
	return nil
}

func savePort(port string) (err error) {
	if err = makeCacheFolder(); err == nil {
		if err = ioutil.WriteFile(
			filepath.Join(os.Getenv("HOME"), ".cache/kpmenu/server.port"),
			[]byte(port),
			0644,
		); err != nil {
			return fmt.Errorf("failed to make server port cache file: %v", err)
		}
	}
	return err
}

func getPort() (string, error) {
	data, err := ioutil.ReadFile(filepath.Join(os.Getenv("HOME"), ".cache/kpmenu/server.port"))
	return string(data), err
}
