package main

import (
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"os"

	"log"
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

		log.Println("Connection from", sshConn.RemoteAddr())
		go func() {
			for chanReq := range newChans {
				go handleChanReq(chanReq)
			}
			log.Println("End of connection")
			sshConn.Close()
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

	client, session, err := connectUpstream()
	if err != nil {
		ch.Write([]byte("fail to connect upstream: " + err.Error() + "\r\n"))
		ch.Close()
		return
	}

	exitStatus, err := pipe(ch, client, session, command)
	if err != nil {
		ch.Write([]byte("fail to pipe command:" + err.Error()))
		ch.Close()
		return
	}

	exitStatusBuffer := make([]byte, 4)
	binary.PutUvarint(exitStatusBuffer, uint64(exitStatus))
	log.Println("forward exit-code", exitStatus, "to client")
	_, err = ch.SendRequest("exit-status", false, exitStatusBuffer)
	if err != nil {
		log.Println("Failed to forward exit-status to client:", err)
	}

	ch.Close()
	client.Close()
	log.Println("End of exec")
}

func pipe(ch ssh.Channel, client *ssh.Client, session *ssh.Session, command string) (int, error) {
	targetStderr, err := session.StderrPipe()
	if err != nil {
		return -1, errors.New("fail to pipe stderr: " + err.Error())
	}
	targetStdout, err := session.StdoutPipe()
	if err != nil {
		return -1, errors.New("fail to pipe stdout: " + err.Error())
	}
	targetStdin, err := session.StdinPipe()
	if err != nil {
		return -1, errors.New("fail to pipe stdin: " + err.Error())
	}

	go io.Copy(targetStdin, ch)
	go io.Copy(ch.Stderr(), targetStderr)
	go io.Copy(ch, targetStdout)

	err = session.Start(command)
	if err != nil {
		ch.Write([]byte("Error when starting '" + command + "': " + err.Error()))
		ch.Close()
	}

	err = session.Wait()
	if err != nil {
		if err, ok := err.(*ssh.ExitError); ok {
			return err.ExitStatus(), nil
		} else {
			return -1, errors.New("failed to wait ssh command: " + err.Error())
		}
	}

	return 0, nil
}
