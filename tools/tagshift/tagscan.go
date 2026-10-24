// tagscan — #3128 forensic dissector for the shifted-tag sub-sector tear
// on sealed killed images. Built into the mkfs.vlfs package via
// `go build -overlay` so it can reuse the tool's own AEAD/superblock/tag-
// region implementation rather than a second, drifting copy.
//
// Usage (through the overlaid binary):
//
//	mkfs.vlfs --tagscan <sealed.img> [--passphrase-file <pass.txt>]
//	mkfs.vlfs --tagscan-hexdump <sealed.img> <home-block>
//
// Per sealed block b in [2, tag_first) the scanner recomputes the AEAD
// tag over the block's STORED ciphertext with its STORED nonce and the
// volume AAD, then classifies the stored 16-byte tag field:
//
//	ok       stored == expected
//	shift    stored == expected[1..15] ++ 0x00        (the #3128 tear)
//	shift-x  stored == expected[1..15] ++ x (x != 0)
//	zero-tag stored tag is all zeros
//	zero-e   whole {nonce,tag} entry is zeros (never written)
//	diverge  any other mismatch
//
// The entry's file offset is printed for every non-ok entry so the tear
// can be located sector-precisely in the raw image.
package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
)

func init() {
	if len(os.Args) < 2 || (os.Args[1] != "--tagscan" && os.Args[1] != "--tagscan-hexdump") {
		return
	}
	fs := flag.NewFlagSet("tagscan", flag.ExitOnError)
	pass := fs.String("passphrase-file", "", "seal passphrase file")
	_ = fs.Parse(os.Args[2:])
	args := fs.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "tagscan: need an image path")
		os.Exit(2)
	}
	if os.Args[1] == "--tagscan-hexdump" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "tagscan-hexdump: need <img> <block>")
			os.Exit(2)
		}
		var blk uint64
		fmt.Sscanf(args[1], "%d", &blk)
		os.Exit(tagHexdump(args[0], uint32(blk)))
	}
	os.Exit(tagscan(args[0], *pass))
}

// pickSuperblock returns the newest valid superblock copy (highest seq).
func pickSuperblock(img []byte) (*superblock, error) {
	var best *superblock
	for slot := uint32(0); slot < 2; slot++ {
		sb, err := parseSuperblock(img, slot)
		if err != nil {
			return nil, err
		}
		if sb != nil && (best == nil || sb.seq > best.seq) {
			best = sb
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no valid superblock")
	}
	return best, nil
}

// tagHomeEntry returns the stored {nonce,tag} slot bytes for sealed
// block b, read from the RAW image (no journal overlay — the forensic
// question is what the file holds).
func tagHomeEntry(img []byte, tagFirst uint32, b uint32) []byte {
	slot := b - 2
	home := tagFirst + slot/TagEntriesPerBlock
	blk := block(img, home)
	off := uint64(slot%TagEntriesPerBlock) * TagEntryBytes
	return blk[off : off+TagEntryBytes]
}

func tagscan(path, passFile string) int {
	img, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tagscan:", err)
		return 2
	}
	sb, err := pickSuperblock(img)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tagscan:", err)
		return 2
	}
	if sb.version != Version2 {
		fmt.Fprintln(os.Stderr, "tagscan: image is not a sealed v2 volume")
		return 2
	}
	var dek *[KeyBytes]byte
	if passFile != "" {
		pass, err := os.ReadFile(passFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tagscan:", err)
			return 2
		}
		pass = bytes.TrimRight(pass, "\r\n")
		dek, err = unwrapDEK(sb, pass)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tagscan: unwrap:", err)
			return 2
		}
	} else {
		fmt.Fprintln(os.Stderr, "tagscan: WARNING no passphrase — expected tags unverifiable (ok/shift undetectable)")
	}

	fmt.Printf("image=%s volID=%x blocks=%d tag_first=%d tag_blocks=%d\n",
		path, sb.volumeID, sb.volumeBlocks, sb.tagFirst, sb.tagBlocks)

	var ok, shift, shiftX, zeroTag, zeroE, diverge int
	var shiftK [16]int // shift-k: stored = exp[k..15] ++ 0^k (k leading zeros of exp dropped)
	for b := uint32(2); b < sb.tagFirst && uint64(b)*BlockLen < uint64(len(img)); b++ {
		e := tagHomeEntry(img, sb.tagFirst, b)
		nonce := e[:aeadNonceLen]
		stored := e[aeadNonceLen:]
		if bytes.Equal(e, make([]byte, TagEntryBytes)) {
			zeroE++
			continue
		}
		if bytes.Equal(stored, make([]byte, aeadTagLen)) {
			zeroTag++
			continue
		}
		if dek == nil {
			continue
		}
		// Recompute the tag the way aeadSeal would have produced it.
		var pkey [32]byte
		var first [chachaBlock]byte
		var n [aeadNonceLen]byte
		copy(n[:], nonce)
		chacha20Block((*[aeadKeyLen]byte)(dek), 0, &n, &first)
		copy(pkey[:], first[:32])
		var exp [aeadTagLen]byte
		aeadMAC(&exp, block(img, b), blockAAD(sb.volumeID, b), &pkey)

		if bytes.Equal(stored, exp[:]) {
			ok++
			continue
		}
		slot := b - 2
		home := sb.tagFirst + slot/TagEntriesPerBlock
		entryOff := uint64(home)*BlockLen + uint64(slot%TagEntriesPerBlock)*TagEntryBytes
		// Minimal-length emission: stored = exp[k..15] ++ 0^k for the k
		// leading zero bytes of exp — the #3128 leading-zero-suppression
		// signature at any suppression depth (T>>8 was the only shape the
		// first scanner named; T>>16 hid inside `diverge`).
		k := -1
		for kk := 15; kk >= 1; kk-- {
			if !bytes.Equal(stored[:aeadTagLen-kk], exp[kk:]) {
				continue
			}
			tailZero := true
			for _, c := range stored[aeadTagLen-kk:] {
				if c != 0 {
					tailZero = false
				}
			}
			if tailZero {
				k = kk
				break
			}
		}
		if k >= 0 {
			shiftK[k]++
			shift++
			fmt.Printf("shift-%d b=%-6d home=%-6d slot=%-3d entry_off=%#x tag_off=%#x exp0=%02x\n      nonce=%s\n      stor=%s\n      exp =%s\n",
				k, b, home, slot%TagEntriesPerBlock, entryOff, entryOff+aeadNonceLen,
				exp[0], hex.EncodeToString(nonce),
				hex.EncodeToString(stored), hex.EncodeToString(exp[:]))
			continue
		}
		if bytes.Equal(stored[:15], exp[1:]) {
			shiftX++
			fmt.Printf("shift-x b=%-6d home=%-6d slot=%-3d tail=%02x exp0=%02x stor=%s exp=%s\n",
				b, home, slot%TagEntriesPerBlock, stored[15], exp[0],
				hex.EncodeToString(stored), hex.EncodeToString(exp[:]))
			continue
		}
		diverge++
		if diverge <= 20 {
			fmt.Printf("diverge b=%-6d home=%-6d slot=%-3d stor=%s exp=%s\n",
				b, home, slot%TagEntriesPerBlock,
				hex.EncodeToString(stored), hex.EncodeToString(exp[:]))
		}
	}
	fmt.Printf("tagscan: ok=%d shift=%d", ok, shift)
	for kk := 1; kk < 16; kk++ {
		if shiftK[kk] > 0 {
			fmt.Printf(" (>>%d:%d)", 8*kk, shiftK[kk])
		}
	}
	fmt.Printf(" shift-x=%d zero-tag=%d zero-entry=%d diverge=%d\n",
		shiftX, zeroTag, zeroE, diverge)
	if shift+diverge+shiftX > 0 {
		return 1
	}
	return 0
}

func tagHexdump(path string, blk uint32) int {
	img, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tagscan-hexdump:", err)
		return 2
	}
	b := block(img, blk)
	for i := 0; i < len(b); i += 16 {
		fmt.Printf("%06x  %s\n", uint64(blk)*BlockLen+uint64(i),
			hex.EncodeToString(b[i:i+16]))
	}
	return 0
}
