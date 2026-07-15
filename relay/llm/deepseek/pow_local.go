package deepseek

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/bits"
	"strconv"
)

var deepSeekRoundConstants = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808a, 0x8000000080008000,
	0x000000000000808b, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008a, 0x0000000000000088, 0x0000000080008009, 0x000000008000000a,
	0x000000008000808b, 0x800000000000008b, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800a, 0x800000008000000a,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

var deepSeekRotationOffsets = [25]int{
	0, 1, 62, 28, 27,
	36, 44, 6, 55, 20,
	3, 10, 43, 25, 39,
	41, 45, 15, 21, 8,
	18, 2, 61, 56, 14,
}

func deepSeekKeccakF23(state *[25]uint64) {
	for round := 1; round < 24; round++ {
		var columns [5]uint64
		for x := 0; x < 5; x++ {
			columns[x] = state[x] ^ state[x+5] ^ state[x+10] ^ state[x+15] ^ state[x+20]
		}
		for x := 0; x < 5; x++ {
			delta := columns[(x+4)%5] ^ bits.RotateLeft64(columns[(x+1)%5], 1)
			for y := 0; y < 5; y++ {
				state[x+5*y] ^= delta
			}
		}

		var rotated [25]uint64
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				source := x + 5*y
				destination := y + 5*((2*x+3*y)%5)
				rotated[destination] = bits.RotateLeft64(state[source], deepSeekRotationOffsets[source])
			}
		}
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				state[x+5*y] = rotated[x+5*y] ^ (^rotated[(x+1)%5+5*y] & rotated[(x+2)%5+5*y])
			}
		}
		state[0] ^= deepSeekRoundConstants[round]
	}
}

func deepSeekHashV1(data []byte) [32]byte {
	const rate = 136
	var state [25]uint64
	offset := 0
	for offset+rate <= len(data) {
		for i := 0; i < rate/8; i++ {
			state[i] ^= binary.LittleEndian.Uint64(data[offset+i*8:])
		}
		deepSeekKeccakF23(&state)
		offset += rate
	}

	var final [rate]byte
	copy(final[:], data[offset:])
	final[len(data)-offset] = 0x06
	final[rate-1] |= 0x80
	for i := 0; i < rate/8; i++ {
		state[i] ^= binary.LittleEndian.Uint64(final[i*8:])
	}
	deepSeekKeccakF23(&state)

	var result [32]byte
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint64(result[i*8:], state[i])
	}
	return result
}

func solveDeepSeekPOW(ctx context.Context, challengeHex, salt string, expireAt, difficulty int64) (int64, error) {
	if len(challengeHex) != 64 {
		return 0, errors.New("DeepSeek PoW challenge must be 64 hexadecimal characters")
	}
	targetBytes, err := hex.DecodeString(challengeHex)
	if err != nil {
		return 0, err
	}
	if difficulty <= 0 {
		return 0, errors.New("DeepSeek PoW difficulty must be positive")
	}

	var target [4]uint64
	for i := range target {
		target[i] = binary.LittleEndian.Uint64(targetBytes[i*8:])
	}
	prefix := []byte(salt + "_" + strconv.FormatInt(expireAt, 10) + "_")
	if len(prefix)+20 >= 136 {
		return 0, errors.New("DeepSeek PoW prefix is too long")
	}
	var baseBlock [136]byte
	copy(baseBlock[:], prefix)

	for answer := int64(0); answer < difficulty; answer++ {
		if answer&0x3ff == 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
			}
		}

		block := baseBlock
		message := strconv.AppendInt(block[:len(prefix)], answer, 10)
		block[len(message)] = 0x06
		block[len(block)-1] |= 0x80
		var state [25]uint64
		for i := 0; i < len(block)/8; i++ {
			state[i] = binary.LittleEndian.Uint64(block[i*8:])
		}
		deepSeekKeccakF23(&state)
		if state[0] == target[0] && state[1] == target[1] && state[2] == target[2] && state[3] == target[3] {
			return answer, nil
		}
	}
	return 0, errors.New("DeepSeek PoW solution not found within difficulty")
}
