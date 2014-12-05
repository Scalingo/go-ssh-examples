package main

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

var (
	hostPrivateKeySigner ssh.Signer
)

func init() {
	keyPath := "./host_key"
	if os.Getenv("HOST_KEY") != "" {
		keyPath = os.Getenv("HOST_KEY")
	}

	hostPrivateKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		panic(err)
	}

	hostPrivateKeySigner, err = ssh.ParsePrivateKey(hostPrivateKey)
	if err != nil {
		panic(err)
	}
}

func keyAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	log.Println(conn.RemoteAddr(), "authenticate with", key.Type())
	return nil, nil
}

func main() {
	config := ssh.ServerConfig{
		PublicKeyCallback: keyAuth,
	}
	config.AddHostKey(hostPrivateKeySigner)

	port := "2222"
	if os.Getenv("PORT") != "" {
		port = os.Getenv("PORT")
	}
	socket, err := net.Listen("tcp", ":"+port)
	if err != nil {
		panic(err)
	}

	for {
		conn, err := socket.Accept()
		if err != nil {
			panic(err)
		}

		// From a standard TCP connection to an encrypted SSH connection
		sshConn, newChans, _, err := ssh.NewServerConn(conn, &config)
		if err != nil {
			panic(err)
		}
		defer sshConn.Close()

		log.Println("Connection from", sshConn.RemoteAddr())
		go func() {
			for chanReq := range newChans {
				go handleChanReq(chanReq)
			}
		}()
	}
}

func handleChanReq(chanReq ssh.NewChannel) {
	if chanReq.ChannelType() != "session" {
		chanReq.Reject(ssh.Prohibited, "channel type is not a session")
		return
	}

	ch, reqs, err := chanReq.Accept()
	if err != nil {
		log.Println("fail to accept channel request", err)
		return
	}

	req := <-reqs
	if req.Type != "exec" {
		ch.Write([]byte("request type '" + req.Type + "' is not 'exec'\r\n"))
		ch.Close()
		return
	}

	handleExec(ch, req)
}

// Payload: int: command size, string: command
func handleExec(ch ssh.Channel, req *ssh.Request) {
	command := string(req.Payload[4:])
	gitCmds := []string{"git-receive-pack", "git-upload-pack"}

	valid := false
	for _, cmd := range gitCmds {
		if strings.HasPrefix(command, cmd) {
			valid = true
		}
	}
	if !valid {
		ch.Write([]byte("command is not a GIT command\r\n"))
		ch.Close()
		return
	}

	ch.Write([]byte("well done!\r\n"))
	ch.Close()
}
