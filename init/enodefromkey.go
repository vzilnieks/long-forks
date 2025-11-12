package main

import (
	"crypto/ecdsa"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: enodefromkey <path-to-nodekey-hex>")
	}
	b, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	hex := strings.TrimSpace(string(b))
	priv, err := crypto.HexToECDSA(hex)
	if err != nil {
		log.Fatal(err)
	}
	pub := priv.Public().(*ecdsa.PublicKey)
	uncompressed := crypto.FromECDSAPub(pub) // 65 bytes, 0x04 + X(32) + Y(32)
	fmt.Printf("%x", uncompressed[1:])
}
