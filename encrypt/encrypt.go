package encrypt

import (
	"crypto/cipher"
	"golang.org/x/crypto/blowfish"
)

type Encryptor struct {
	IV     []byte
	Key    []byte
	Mode   string
	Cipher string
}

const ENC_IV_SIZE = 8

func (c *Encryptor) Encrypt(plainText []byte) ([]byte, error) {
	var blockCipher cipher.Block
	var cipherMode cipher.BlockMode
	var err error
	switch c.Mode {
	default:
		blockCipher, err = blowfish.NewCipher(c.Key)
		if err != nil {
			return nil, err
		}
		switch c.Mode {
		default:
			cipherMode = cipher.NewCBCEncrypter(blockCipher, c.IV)
		}
	}
	textLen := len(plainText)
	modLen := blockCipher.BlockSize() - (textLen % blockCipher.BlockSize())
	plainText = append(plainText, make([]byte, modLen)...)
	encryptedText := make([]byte, textLen+modLen)
	cipherMode.CryptBlocks(encryptedText, plainText)
	return encryptedText, nil
}

func (c *Encryptor) Decrypt(encryptedText []byte) ([]byte, error) {
	var blockCipher cipher.Block
	var cipherMode cipher.BlockMode
	var err error
	switch c.Mode {
	default:
		blockCipher, err = blowfish.NewCipher(c.Key)
		if err != nil {
			return nil, err
		}
		switch c.Mode {
		default:
			cipherMode = cipher.NewCBCDecrypter(blockCipher, c.IV)
		}
	}
	plainText := make([]byte, len(encryptedText))
	cipherMode.CryptBlocks(plainText, encryptedText)
	textLen := len(plainText)
	for textLen > 0 {
		if plainText[textLen-1] != 0 {
			break
		}
		textLen -= 1
	}
	plainText = plainText[:textLen]
	return plainText, nil
}
