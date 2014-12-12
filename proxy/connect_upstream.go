package main

import (
	"io/ioutil"
	"log"
	"os/user"

	"golang.org/x/crypto/ssh"
)

var (
	username string
	key      ssh.Signer
)

func init() {
	u, err := user.Current()
	if err != nil {
		log.Fatalf("fail to get current user: %v", err)
	}
	username = u.Username

	keyContent, err := ioutil.ReadFile("./user_key")
	if err != nil {
		log.Fatalln(err)
	}

	key, err = ssh.ParsePrivateKey(keyContent)
	if err != nil {
		log.Fatalln(err)
	}
}

func connectUpstream() (*ssh.Client, *ssh.Session, error) {
	sshConfig := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(key)},
	}

	client, err := ssh.Dial("tcp", "localhost:2223", sshConfig)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	return client, session, nil
}
