/*
 * Copyright (C) 2019 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */
package oep4

import (
	"fmt"
	"io"
	"math/big"

	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/smartcontract/service/native/utils"
)

type AssetId uint64

const (
	XSHARD_TRANSFER_PENDING  uint8 = 0x06
	XSHARD_TRANSFER_COMPLETE uint8 = 0x07
)

type XShardTransferState struct {
	Id        *big.Int       `json:"id"`
	ToShard   common.ShardID `json:"to_shard"`
	ToAccount common.Address `json:"to_account"`
	Amount    *big.Int       `json:"amount"`
	Status    uint8          `json:"status"`
}

func (this *XShardTransferState) Serialization(sink *common.ZeroCopySink) {
	sink.WriteVarBytes(common.BigIntToNeoBytes(this.Id))
	utils.SerializationShardId(sink, this.ToShard)
	sink.WriteAddress(this.ToAccount)
	sink.WriteVarBytes(common.BigIntToNeoBytes(this.Amount))
	sink.WriteUint8(this.Status)
}

func (this *XShardTransferState) Deserialization(source *common.ZeroCopySource) error {
	var err error = nil
	id, _, irr, eof := source.NextVarBytes()
	if irr {
		return common.ErrIrregularData
	}
	if eof {
		return io.ErrUnexpectedEOF
	}
	this.Id = common.BigIntFromNeoBytes(id)
	this.ToShard, err = utils.DeserializationShardId(source)
	if err != nil {
		return fmt.Errorf("deserialization: read to shard failed, err: %s", err)
	}
	this.ToAccount, eof = source.NextAddress()
	if eof {
		return io.ErrUnexpectedEOF
	}
	amount, _, irr, eof := source.NextVarBytes()
	if irr {
		return common.ErrIrregularData
	}
	if eof {
		return io.ErrUnexpectedEOF
	}
	this.Amount = common.BigIntFromNeoBytes(amount)
	this.Status, eof = source.NextUint8()
	if eof {
		return io.ErrUnexpectedEOF
	}
	return nil
}
