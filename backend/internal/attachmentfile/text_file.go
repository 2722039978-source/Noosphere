package attachmentfile

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	textunicode "golang.org/x/text/encoding/unicode"
)

var errMalformedUTF8 = errors.New(encodingProblem)

var ErrSourceChanged = errors.New("附件原件与保存的 SHA-256 不一致，请重新选择文件")

// DetectTextEncoding validates the full file once and records how it was
// decoded so subsequent searches can read it in a single pass.
func DetectTextEncoding(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	var prefix [2]byte
	_, err = io.ReadFull(file, prefix[:])
	closeErr := file.Close()
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	encoding := "utf-8"
	switch prefix {
	case [2]byte{0xff, 0xfe}:
		encoding = "utf-16le"
	case [2]byte{0xfe, 0xff}:
		encoding = "utf-16be"
	}
	err = ScanTextFile(ctx, path, encoding, nil)
	if encoding == "utf-8" && errors.Is(err, errMalformedUTF8) {
		encoding = "gb18030"
		err = ScanTextFile(ctx, path, encoding, nil)
	}
	return encoding, err
}

// ScanTextFile emits normalized text without allocating a buffer the size of
// the attachment. A nil visitor validates the file and its declared encoding.
func ScanTextFile(ctx context.Context, path, encoding string, visit func(rune)) error {
	return scanTextFile(ctx, path, encoding, "", visit)
}

// ScanVerifiedTextFile checks the same raw bytes used to produce the excerpts.
// Callers must discard the excerpts and any side effects of visit on error.
func ScanVerifiedTextFile(ctx context.Context, path, encoding, expectedHash string, visit func(rune)) error {
	return scanTextFile(ctx, path, encoding, expectedHash, visit)
}

func scanTextFile(ctx context.Context, path, encoding, expectedHash string, visit func(rune)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var input io.Reader = io.LimitReader(file, MaxBytes+1)
	hash := sha256.New()
	if expectedHash != "" {
		input = io.TeeReader(input, hash)
	}
	if err := scanText(ctx, input, encoding, visit); err != nil {
		return err
	}
	if expectedHash != "" && fmt.Sprintf("%x", hash.Sum(nil)) != expectedHash {
		return ErrSourceChanged
	}
	return nil
}

func scanText(ctx context.Context, input io.Reader, encoding string, visit func(rune)) error {
	const bufferSize = 64 << 10
	raw := bufio.NewReaderSize(input, bufferSize)
	reader := raw
	switch encoding {
	case "utf-8":
		if prefix, _ := raw.Peek(3); bytes.Equal(prefix, []byte{0xef, 0xbb, 0xbf}) {
			raw.Discard(3)
		}
	case "utf-16le", "utf-16be":
		endian := textunicode.LittleEndian
		bom := []byte{0xff, 0xfe}
		if encoding == "utf-16be" {
			endian = textunicode.BigEndian
			bom = []byte{0xfe, 0xff}
		}
		if prefix, _ := raw.Peek(2); !bytes.Equal(prefix, bom) {
			return fmt.Errorf("%s", encodingProblem)
		}
		raw.Discard(2)
		reader = bufio.NewReaderSize(textunicode.UTF16(endian, textunicode.IgnoreBOM).NewDecoder().Reader(raw), bufferSize)
	case "gb18030":
		reader = bufio.NewReaderSize(simplifiedchinese.GB18030.NewDecoder().Reader(raw), bufferSize)
	default:
		return fmt.Errorf("%s", encodingProblem)
	}

	hasText, prevCR := false, false
	for n := 0; ; n++ {
		if n%8192 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		r, width, err := reader.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if r == utf8.RuneError && width == 1 && encoding == "utf-8" {
			return errMalformedUTF8
		}
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\r' && r != '\n' && r != '\t') {
			return fmt.Errorf("%s", encodingProblem)
		}
		hasText = hasText || !unicode.IsSpace(r)
		if visit != nil && !(prevCR && r == '\n') {
			if r == '\r' {
				visit('\n')
			} else {
				visit(r)
			}
		}
		prevCR = r == '\r'
	}
	if !hasText {
		return fmt.Errorf("文件没有可用文字")
	}
	return ctx.Err()
}
