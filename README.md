# SSH Client/Server example with Go

## Initialization

To setup a SSH server, a host ssh keypair (usually RSA) has to be created, to do so, run:

```
bash init.sh
```

Those files should have been created in the project directory:

* `./host_key`
* `./host_key.pub`

## Simple client usage

```
go run client.go <user> <server:port> <command>
```

Example:

```
└> go run client.go foobar example.com:22 'ls /'
Password: *********
bin
boot
conf.d
dev
etc
home
initrd.img
lib
lib64
lost+found
media
mnt
opt
proc
root
run
sbin
srv
sys
tmp
usr
var
vmlinuz
```

### Notes:

Please create issues, if you want more details.
