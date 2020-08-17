package stratum

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"log"
)

/*type BlockTemplate struct {
	diffInt64      int64
	height         int64
	difficulty     *big.Int
	reservedOffset int
	prevHash       string
	buffer         []byte
}*/

type BlockTemplate struct {
	Blocktemplate_blob string
	Blockhashing_blob  string
	Expected_reward    uint64
	Difficulty         uint64
	Height             uint64
	Prev_Hash          string
	Reserved_Offset    uint64
	Epoch              uint64
	Status             string
	Buffer             []byte
}

func (b *BlockTemplate) nextBlob(extraNonce uint32, instanceId []byte) string {
	extraBuff := new(bytes.Buffer)
	binary.Write(extraBuff, binary.BigEndian, extraNonce)

	blobBuff := make([]byte, len(b.Buffer))
	copy(blobBuff, b.Buffer)
	copy(blobBuff[b.Reserved_Offset+4:b.Reserved_Offset+7], instanceId)
	copy(blobBuff[b.Reserved_Offset:], extraBuff.Bytes())
	//blob := cnutil.ConvertBlob(blobBuff)
	blob := blobBuff
	return hex.EncodeToString(blob)
}

func (s *StratumServer) fetchBlockTemplate() bool {
	r := s.rpc()
	reply, err := r.GetBlockTemplate(10, s.config.Address)
	if err != nil {
		log.Printf("Error while refreshing block template: %s", err)
		return false
	}
	t := s.currentBlockTemplate()

	if t != nil && t.Prev_Hash == reply.Prev_Hash {
		// Fallback to height comparison
		if len(reply.Prev_Hash) == 0 && reply.Height > t.Height {
			log.Printf("New block to mine on %s at height %v, diff: %v", r.Name, reply.Height, reply.Difficulty)
		} else {
			return false
		}
	} else {
		log.Printf("New block to mine on %s at height %v, diff: %v, prev_hash: %s", r.Name, reply.Height, reply.Difficulty, reply.Prev_Hash)
	}
	/*newTemplate := BlockTemplate{
		diffInt64:      reply.Difficulty,
		difficulty:     big.NewInt(reply.Difficulty),
		height:         reply.Height,
		prevHash:       reply.PrevHash,
		reservedOffset: reply.ReservedOffset,
	}
	newTemplate.buffer, _ = hex.DecodeString(reply.Blob)*/

	newTemplate := BlockTemplate{
		Blocktemplate_blob: reply.Blocktemplate_blob,
		Blockhashing_blob:  reply.Blockhashing_blob,
		Expected_reward:    reply.Expected_reward,
		Difficulty:         reply.Difficulty,
		Height:             reply.Height,
		Prev_Hash:          reply.Prev_Hash,
		Reserved_Offset:    reply.Reserved_Offset,
		Epoch:              reply.Epoch,
		Status:             reply.Status,
	}
	newTemplate.Buffer, _ = hex.DecodeString(reply.Blockhashing_blob) // Invalid PoW
	//newTemplate.Buffer, _ = hex.DecodeString(reply.Blocktemplate_blob) // Timestamp in future
	s.blockTemplate.Store(&newTemplate)
	return true
}
