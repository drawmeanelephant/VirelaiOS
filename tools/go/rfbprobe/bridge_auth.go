package main

import (
	"crypto/des"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"os"
)

// VNC auth uses only the first eight password bytes. The bridge generates
// exactly eight printable characters and accepts one loopback viewer.
func randomVNCPassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	var password [8]byte
	for i := range password {
		for {
			var b [1]byte
			if _, err := rand.Read(b[:]); err != nil {
				return "", err
			}
			limit := 256 / len(alphabet) * len(alphabet)
			if int(b[0]) < limit {
				password[i] = alphabet[int(b[0])%len(alphabet)]
				break
			}
		}
	}
	return string(password[:]), nil
}

// The tape reads a FIFO instead of putting the password in runner.log.
// Running the bridge directly prints it to stderr (the operator's terminal).
func deliverPassword(fifo, password string) error {
	if fifo == "" {
		fmt.Fprintf(os.Stderr, "RFBPROBE: bridge one-shot VNC password: %s\n", password)
		return nil
	}
	info, err := os.Lstat(fifo)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeNamedPipe == 0 || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("bridge password destination must be a private named pipe")
	}
	f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(f, password)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func vncResponse(password string, challenge [16]byte) ([16]byte, error) {
	var key [8]byte
	for i := range key {
		if i < len(password) {
			key[i] = bits.Reverse8(password[i])
		}
	}
	cipher, err := des.NewCipher(key[:])
	if err != nil {
		return [16]byte{}, err
	}
	var response [16]byte
	cipher.Encrypt(response[:8], challenge[:8])
	cipher.Encrypt(response[8:], challenge[8:])
	return response, nil
}

// authenticateViewer speaks RFB 3.3, 3.7, or 3.8 VNC auth (type 2) and
// refuses every other selection. Nothing is forwarded to the guest until
// the challenge response matches in constant time.
func authenticateViewer(viewer io.ReadWriter, password string) error {
	if err := write(viewer, []byte("RFB 003.008\n")); err != nil {
		return err
	}
	var banner [12]byte
	if _, err := io.ReadFull(viewer, banner[:]); err != nil {
		return err
	}
	version := string(banner[:])
	switch version {
	case "RFB 003.003\n":
		if err := write(viewer, []byte{0, 0, 0, 2}); err != nil {
			return err
		}
	case "RFB 003.007\n", "RFB 003.008\n":
		if err := write(viewer, []byte{1, 2}); err != nil {
			return err
		}
		var selection [1]byte
		if _, err := io.ReadFull(viewer, selection[:]); err != nil {
			return err
		}
		if selection[0] != 2 {
			_ = authRefused(viewer, version, "security type refused")
			return fmt.Errorf("security type %d refused", selection[0])
		}
	default:
		return fmt.Errorf("unsupported viewer version %q", banner)
	}
	var challenge [16]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return err
	}
	if err := write(viewer, challenge[:]); err != nil {
		return err
	}
	var answer [16]byte
	if _, err := io.ReadFull(viewer, answer[:]); err != nil {
		return err
	}
	expected, err := vncResponse(password, challenge)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(answer[:], expected[:]) != 1 {
		_ = authRefused(viewer, version, "wrong password")
		return fmt.Errorf("wrong password")
	}
	if err := write(viewer, []byte{0, 0, 0, 0}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "RFBPROBE: bridge viewer VNC auth accepted")
	return nil
}

func authRefused(viewer io.Writer, version, reason string) error {
	result := []byte{0, 0, 0, 1}
	if version == "RFB 003.008\n" {
		result = binary.BigEndian.AppendUint32(result, uint32(len(reason)))
		result = append(result, reason...)
	}
	return write(viewer, result)
}

// The authenticated viewer sees ServerInit and later frames unmodified.
// Only the greeting, security exchange, and ClientInit differ on each
// side. The guest still receives exactly RFB 3.8/None.
func negotiateGuest(viewer io.ReadWriter, guestIn io.Reader, guestOut io.Writer) (byte, error) {
	if err := readBanner(guestIn); err != nil {
		return 0, err
	}
	if err := write(guestOut, []byte("RFB 003.008\n")); err != nil {
		return 0, err
	}
	if err := readNoneOffer(guestIn); err != nil {
		return 0, err
	}
	if err := write(guestOut, []byte{1}); err != nil {
		return 0, err
	}
	var result [4]byte
	if _, err := io.ReadFull(guestIn, result[:]); err != nil {
		return 0, err
	}
	if result != [4]byte{} {
		return 0, fmt.Errorf("guest security result %x", result)
	}
	var shared [1]byte
	if _, err := io.ReadFull(viewer, shared[:]); err != nil {
		return 0, err
	}
	if shared[0] > 1 {
		return 0, fmt.Errorf("invalid ClientInit %d", shared[0])
	}
	if err := write(guestOut, shared[:]); err != nil {
		return 0, err
	}
	return shared[0], nil
}
