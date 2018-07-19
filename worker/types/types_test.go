/*
 * Copyright 2018 The ThunderDB Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package types

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"gitlab.com/thunderdb/ThunderDB/crypto/asymmetric"
	"gitlab.com/thunderdb/ThunderDB/crypto/hash"
	"gitlab.com/thunderdb/ThunderDB/kayak"
	"gitlab.com/thunderdb/ThunderDB/proto"
	"gitlab.com/thunderdb/ThunderDB/sqlchain/storage"
)

func getCommKeys() (*asymmetric.PrivateKey, *asymmetric.PublicKey) {
	testPriv := []byte{
		0xea, 0xf0, 0x2c, 0xa3, 0x48, 0xc5, 0x24, 0xe6,
		0x39, 0x26, 0x55, 0xba, 0x4d, 0x29, 0x60, 0x3c,
		0xd1, 0xa7, 0x34, 0x7d, 0x9d, 0x65, 0xcf, 0xe9,
		0x3c, 0xe1, 0xeb, 0xff, 0xdc, 0xa2, 0x26, 0x94,
	}
	return asymmetric.PrivKeyFromBytes(testPriv)
}

type MyTestBytes []byte

func (bytes MyTestBytes) Serialize() (res []byte) {
	res = make([]byte, len(bytes))
	copy(res, bytes[:])
	return
}

func Test_buildHash(t *testing.T) {
	Convey("build", t, func() {
		var a, b hash.Hash
		var tb MyTestBytes = []byte("test")
		buildHash(tb, &a)
		b = hash.THashH([]byte("test"))
		So(a, ShouldResemble, b)
	})

	Convey("test verify", t, func() {
		var a, b hash.Hash
		var tb MyTestBytes = []byte("test")
		var err error
		buildHash(tb, &a)
		err = verifyHash(tb, &a)
		So(err, ShouldBeNil)
		err = verifyHash(tb, &b)
		So(err, ShouldNotBeNil)
	})
}

func TestSignedRequestHeader_Sign(t *testing.T) {
	privKey, pubKey := getCommKeys()

	Convey("sign", t, func() {
		req := &SignedRequestHeader{
			RequestHeader: RequestHeader{
				QueryType:    WriteQuery,
				NodeID:       proto.NodeID("node"),
				DatabaseID:   proto.DatabaseID("db1"),
				ConnectionID: uint64(1),
				SeqNo:        uint64(2),
				Timestamp:    time.Now().UTC(),
			},
		}

		var err error

		// without signee
		err = req.Sign(privKey)
		So(err, ShouldNotBeNil)

		req.Signee = pubKey
		err = req.Sign(privKey)
		So(err, ShouldBeNil)

		Convey("verify", func() {
			err = req.Verify()
			So(err, ShouldBeNil)

			// modify structure
			req.Timestamp = req.Timestamp.Add(time.Second)

			err = req.Verify()
			So(err, ShouldNotBeNil)
		})
	})
}

func TestRequest_Sign(t *testing.T) {
	privKey, pubKey := getCommKeys()

	Convey("sign", t, func() {
		req := &Request{
			Header: SignedRequestHeader{
				RequestHeader: RequestHeader{
					QueryType:    WriteQuery,
					NodeID:       proto.NodeID("node"),
					DatabaseID:   proto.DatabaseID("db1"),
					ConnectionID: uint64(1),
					SeqNo:        uint64(2),
					Timestamp:    time.Now().UTC(),
				},
				Signee: pubKey,
			},
			Payload: RequestPayload{
				Queries: []storage.Query{
					{
						Pattern: "INSERT INTO test VALUES(1)",
					},
					{
						Pattern: "INSERT INTO test VALUES(2)",
					},
				},
			},
		}

		var err error

		// sign
		err = req.Sign(privKey)
		So(err, ShouldBeNil)
		So(req.Header.BatchCount, ShouldEqual, uint64(len(req.Payload.Queries)))

		// test queries hash
		err = verifyHash(&req.Payload, &req.Header.QueriesHash)
		So(err, ShouldBeNil)

		Convey("serialize", func() {
			So(req.Serialize(), ShouldNotBeEmpty)
			So((*Request)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*RequestHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*RequestPayload)(nil).Serialize(), ShouldNotBeEmpty)
			So((*SignedRequestHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})

			// test nils
			req.Header.Signee = nil
			req.Header.Signature = nil

			So(req.Serialize(), ShouldNotBeEmpty)
		})

		Convey("verify", func() {
			err = req.Verify()
			So(err, ShouldBeNil)

			Convey("header change", func() {
				// modify structure
				req.Header.Timestamp = req.Header.Timestamp.Add(time.Second)

				err = req.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("header change without signing", func() {
				req.Header.Timestamp = req.Header.Timestamp.Add(time.Second)

				buildHash(&req.Header.RequestHeader, &req.Header.HeaderHash)
				err = req.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("header change with invalid queries hash", func() {
				req.Payload.Queries = append(req.Payload.Queries,
					storage.Query{
						Pattern: "select 1",
					},
				)

				err = req.Verify()
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestResponse_Sign(t *testing.T) {
	privKey, pubKey := getCommKeys()

	Convey("sign", t, func() {
		res := &Response{
			Header: SignedResponseHeader{
				ResponseHeader: ResponseHeader{
					Request: SignedRequestHeader{
						RequestHeader: RequestHeader{
							QueryType:    WriteQuery,
							NodeID:       proto.NodeID("node1"),
							DatabaseID:   proto.DatabaseID("db1"),
							ConnectionID: uint64(1),
							SeqNo:        uint64(2),
							Timestamp:    time.Now().UTC(),
						},
						Signee: pubKey,
					},
					NodeID:    proto.NodeID("node2"),
					Timestamp: time.Now().UTC(),
					RowCount:  uint64(1),
				},
				Signee: pubKey,
			},
			Payload: ResponsePayload{
				Columns: []string{
					"test_integer",
					"test_boolean",
					"test_time",
					"test_nil",
					"test_float",
					"test_binary_string",
					"test_string",
				},
				DeclTypes: []string{
					"INTEGER",
					"BOOLEAN",
					"DATETIME",
					"INTEGER",
					"FLOAT",
					"BLOB",
					"TEXT",
				},
				Rows: []ResponseRow{
					{
						Values: []interface{}{
							int(1),
							true,
							time.Now().UTC(),
							nil,
							float64(1.0001),
							"11111\0001111111",
							"11111111111111",
						},
					},
				},
			},
		}

		var data []byte
		var err error
		var rres Response

		// sign directly, embedded original request is not filled
		err = res.Sign(privKey)
		So(err, ShouldNotBeNil)
		So(err, ShouldBeIn, []error{
			ErrSignVerification,
			ErrHashVerification,
		})

		// sign original request first
		err = res.Header.Request.Sign(privKey)
		So(err, ShouldBeNil)

		// sign again
		err = res.Sign(privKey)
		So(err, ShouldBeNil)

		// test hash
		err = verifyHash(&res.Payload, &res.Header.DataHash)
		So(err, ShouldBeNil)

		Convey("serialize", func() {
			So(res.Serialize(), ShouldNotBeEmpty)
			So((*Response)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*ResponseRow)(nil).Serialize(), ShouldNotBeEmpty)
			So((*ResponseHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*ResponsePayload)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*SignedResponseHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})

			data, err = res.Header.MarshalBinary()
			So(err, ShouldBeNil)
			err = rres.Header.UnmarshalBinary(data)
			So(err, ShouldBeNil)
			So(&res.Header, ShouldResemble, &rres.Header)

			// test nils
			res.Header.Signee = nil
			res.Header.Signature = nil

			So(res.Serialize(), ShouldNotBeEmpty)
		})

		// verify
		Convey("verify", func() {
			err = res.Verify()
			So(err, ShouldBeNil)

			Convey("request change", func() {
				res.Header.Request.BatchCount = 200

				err = res.Verify()
				So(err, ShouldNotBeNil)
			})
			Convey("payload change", func() {
				res.Payload.DeclTypes[0] = "INT"

				err = res.Verify()
				So(err, ShouldNotBeNil)
			})
			Convey("header change", func() {
				res.Header.Timestamp = res.Header.Timestamp.Add(time.Second)

				err = res.Verify()
				So(err, ShouldNotBeNil)
			})
			Convey("header change without signing", func() {
				res.Header.Timestamp = res.Header.Timestamp.Add(time.Second)
				buildHash(&res.Header.ResponseHeader, &res.Header.HeaderHash)

				err = res.Verify()
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestAck_Sign(t *testing.T) {
	privKey, pubKey := getCommKeys()

	Convey("sign", t, func() {
		ack := &Ack{
			Header: SignedAckHeader{
				AckHeader: AckHeader{
					Response: SignedResponseHeader{
						ResponseHeader: ResponseHeader{
							Request: SignedRequestHeader{
								RequestHeader: RequestHeader{
									QueryType:    WriteQuery,
									NodeID:       proto.NodeID("node1"),
									DatabaseID:   proto.DatabaseID("db1"),
									ConnectionID: uint64(1),
									SeqNo:        uint64(2),
									Timestamp:    time.Now().UTC(),
								},
								Signee: pubKey,
							},
							NodeID:    proto.NodeID("node2"),
							Timestamp: time.Now().UTC(),
							RowCount:  uint64(1),
						},
						Signee: pubKey,
					},
					NodeID:    proto.NodeID("node1"),
					Timestamp: time.Now().UTC(),
				},
				Signee: pubKey,
			},
		}

		var data []byte
		var err error
		var rack Ack

		// sign directly, embedded original response is not filled
		err = ack.Sign(privKey)
		So(err, ShouldNotBeNil)
		So(err, ShouldBeIn, []error{
			ErrSignVerification,
			ErrHashVerification,
		})

		// sign nested structure, step by step
		// this is not required during runtime
		// during runtime, nested structures is signed and provided by peers
		err = ack.Header.Response.Request.Sign(privKey)
		So(err, ShouldBeNil)
		err = ack.Header.Response.Sign(privKey)
		So(err, ShouldBeNil)
		err = ack.Sign(privKey)
		So(err, ShouldBeNil)

		Convey("serialize", func() {
			So(ack.Serialize(), ShouldNotBeEmpty)
			So((*Ack)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*AckHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*SignedAckHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})

			data, err = ack.Header.MarshalBinary()
			So(err, ShouldBeNil)
			err = rack.Header.UnmarshalBinary(data)
			So(err, ShouldBeNil)
			So(&ack.Header, ShouldResemble, &rack.Header)

			// test nils
			ack.Header.Signee = nil
			ack.Header.Signature = nil

			So(ack.Serialize(), ShouldNotBeEmpty)
		})

		Convey("verify", func() {
			err = ack.Verify()
			So(err, ShouldBeNil)

			Convey("request change", func() {
				ack.Header.Response.Request.QueryType = ReadQuery

				err = ack.Verify()
				So(err, ShouldNotBeNil)
			})
			Convey("response change", func() {
				ack.Header.Response.RowCount = 100

				err = ack.Verify()
				So(err, ShouldNotBeNil)
			})
			Convey("header change", func() {
				ack.Header.Timestamp = ack.Header.Timestamp.Add(time.Second)

				err = ack.Verify()
				So(err, ShouldNotBeNil)
			})
			Convey("header change without signing", func() {
				ack.Header.Timestamp = ack.Header.Timestamp.Add(time.Second)

				buildHash(&ack.Header.AckHeader, &ack.Header.HeaderHash)

				err = ack.Verify()
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestNoAckReport_Sign(t *testing.T) {
	privKey, pubKey := getCommKeys()

	Convey("sign", t, func() {
		noAck := &NoAckReport{
			Header: SignedNoAckReportHeader{
				NoAckReportHeader: NoAckReportHeader{
					NodeID:    proto.NodeID("node2"),
					Timestamp: time.Now().UTC(),
					Response: SignedResponseHeader{
						ResponseHeader: ResponseHeader{
							Request: SignedRequestHeader{
								RequestHeader: RequestHeader{
									QueryType:    WriteQuery,
									NodeID:       proto.NodeID("node1"),
									DatabaseID:   proto.DatabaseID("db1"),
									ConnectionID: uint64(1),
									SeqNo:        uint64(2),
									Timestamp:    time.Now().UTC(),
								},
								Signee: pubKey,
							},
							NodeID:    proto.NodeID("node2"),
							Timestamp: time.Now().UTC(),
							RowCount:  uint64(1),
						},
						Signee: pubKey,
					},
				},
				Signee: pubKey,
			},
		}

		var err error

		// sign directly, embedded original response/request is not filled
		err = noAck.Sign(privKey)
		So(err, ShouldNotBeNil)
		So(err, ShouldBeIn, []error{
			ErrSignVerification,
			ErrHashVerification,
		})

		// sign nested structure
		err = noAck.Header.Response.Request.Sign(privKey)
		So(err, ShouldBeNil)
		err = noAck.Header.Response.Sign(privKey)
		So(err, ShouldBeNil)
		err = noAck.Sign(privKey)
		So(err, ShouldBeNil)

		Convey("serialize", func() {
			So(noAck.Serialize(), ShouldNotBeEmpty)
			So((*NoAckReport)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*NoAckReportHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*SignedNoAckReportHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})

			// test nils
			noAck.Header.Signee = nil
			noAck.Header.Signature = nil

			So(noAck.Serialize(), ShouldNotBeEmpty)
		})

		Convey("verify", func() {
			err = noAck.Verify()
			So(err, ShouldBeNil)

			Convey("request change", func() {
				noAck.Header.Response.Request.QueryType = ReadQuery

				err = noAck.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("response change", func() {
				noAck.Header.Response.RowCount = 100

				err = noAck.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("header change", func() {
				noAck.Header.Timestamp = noAck.Header.Timestamp.Add(time.Second)

				err = noAck.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("header change without signing", func() {
				noAck.Header.Timestamp = noAck.Header.Timestamp.Add(time.Second)

				buildHash(&noAck.Header.NoAckReportHeader, &noAck.Header.HeaderHash)

				err = noAck.Verify()
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestAggrNoAckReport_Sign(t *testing.T) {
	privKey, pubKey := getCommKeys()

	Convey("sign", t, func() {
		aggrNoAck := &AggrNoAckReport{
			Header: SignedAggrNoAckReportHeader{
				AggrNoAckReportHeader: AggrNoAckReportHeader{
					NodeID:    proto.NodeID("node3"),
					Timestamp: time.Now().UTC(),
					Reports: []SignedNoAckReportHeader{
						{
							NoAckReportHeader: NoAckReportHeader{
								NodeID:    proto.NodeID("node2"),
								Timestamp: time.Now().UTC(),
								Response: SignedResponseHeader{
									ResponseHeader: ResponseHeader{
										Request: SignedRequestHeader{
											RequestHeader: RequestHeader{
												QueryType:    WriteQuery,
												NodeID:       proto.NodeID("node1"),
												DatabaseID:   proto.DatabaseID("db1"),
												ConnectionID: uint64(1),
												SeqNo:        uint64(2),
												Timestamp:    time.Now().UTC(),
											},
											Signee: pubKey,
										},
										NodeID:    proto.NodeID("node2"),
										Timestamp: time.Now().UTC(),
										RowCount:  uint64(1),
									},
									Signee: pubKey,
								},
							},
							Signee: pubKey,
						},
						{
							NoAckReportHeader: NoAckReportHeader{
								NodeID:    proto.NodeID("node3"),
								Timestamp: time.Now().UTC(),
								Response: SignedResponseHeader{
									ResponseHeader: ResponseHeader{
										Request: SignedRequestHeader{
											RequestHeader: RequestHeader{
												QueryType:    WriteQuery,
												NodeID:       proto.NodeID("node1"),
												DatabaseID:   proto.DatabaseID("db1"),
												ConnectionID: uint64(1),
												SeqNo:        uint64(2),
												Timestamp:    time.Now().UTC(),
											},
											Signee: pubKey,
										},
										NodeID:    proto.NodeID("node3"),
										Timestamp: time.Now().UTC(),
										RowCount:  uint64(1),
									},
									Signee: pubKey,
								},
							},
							Signee: pubKey,
						},
					},
					Peers: &kayak.Peers{
						Term: uint64(1),
						Leader: &kayak.Server{
							Role: proto.Leader,
							ID:   proto.NodeID("node3"),
						},
						Servers: []*kayak.Server{
							{
								Role: proto.Leader,
								ID:   proto.NodeID("node3"),
							},
							{
								Role: proto.Follower,
								ID:   proto.NodeID("node2"),
							},
						},
					},
				},
				Signee: pubKey,
			},
		}

		var err error

		// sign directly, embedded original response/request is not filled
		err = aggrNoAck.Sign(privKey)
		So(err, ShouldNotBeNil)
		So(err, ShouldBeIn, []error{
			ErrSignVerification,
			ErrHashVerification,
		})

		// sign nested structure
		err = aggrNoAck.Header.Reports[0].Response.Request.Sign(privKey)
		So(err, ShouldBeNil)
		err = aggrNoAck.Header.Reports[1].Response.Request.Sign(privKey)
		So(err, ShouldBeNil)
		err = aggrNoAck.Header.Reports[0].Response.Sign(privKey)
		So(err, ShouldBeNil)
		err = aggrNoAck.Header.Reports[1].Response.Sign(privKey)
		So(err, ShouldBeNil)
		err = aggrNoAck.Header.Reports[0].Sign(privKey)
		So(err, ShouldBeNil)
		err = aggrNoAck.Header.Reports[1].Sign(privKey)
		So(err, ShouldBeNil)
		err = aggrNoAck.Sign(privKey)
		So(err, ShouldBeNil)

		Convey("serialize", func() {
			So(aggrNoAck.Serialize(), ShouldNotBeEmpty)
			So((*AggrNoAckReport)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*AggrNoAckReportHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})
			So((*SignedAggrNoAckReportHeader)(nil).Serialize(), ShouldResemble, []byte{'\000'})

			// test nils
			aggrNoAck.Header.Signee = nil
			aggrNoAck.Header.Signature = nil

			So(aggrNoAck.Serialize(), ShouldNotBeEmpty)
		})

		Convey("verify", func() {
			err = aggrNoAck.Verify()
			So(err, ShouldBeNil)

			Convey("request change", func() {
				aggrNoAck.Header.Reports[0].Response.Request.QueryType = ReadQuery

				err = aggrNoAck.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("response change", func() {
				aggrNoAck.Header.Reports[0].Response.RowCount = 1000

				err = aggrNoAck.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("report change", func() {
				aggrNoAck.Header.Reports[0].Timestamp = aggrNoAck.Header.Reports[0].Timestamp.Add(time.Second)

				err = aggrNoAck.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("header change", func() {
				aggrNoAck.Header.Timestamp = aggrNoAck.Header.Timestamp.Add(time.Second)

				err = aggrNoAck.Verify()
				So(err, ShouldNotBeNil)
			})

			Convey("header change without signing", func() {
				aggrNoAck.Header.Timestamp = aggrNoAck.Header.Timestamp.Add(time.Second)

				buildHash(&aggrNoAck.Header.AggrNoAckReportHeader, &aggrNoAck.Header.HeaderHash)

				err = aggrNoAck.Verify()
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestInitService(t *testing.T) {
	Convey("test create", t, func() {
		_ = &InitService{}
		_ = &InitServiceResponse{
			Instances: []ServiceInstance{
				{
					DatabaseID: proto.DatabaseID("db1"),
					Peers: &kayak.Peers{
						Term: uint64(1),
						Leader: &kayak.Server{
							Role: proto.Leader,
							ID:   proto.NodeID("node3"),
						},
						Servers: []*kayak.Server{
							{
								Role: proto.Leader,
								ID:   proto.NodeID("node3"),
							},
							{
								Role: proto.Follower,
								ID:   proto.NodeID("node2"),
							},
						},
						PubKey:    nil,
						Signature: nil,
					},
					GenesisBlock: nil,
				},
			},
		}
	})
}

func TestUpdateService(t *testing.T) {
	Convey("test create", t, func() {
		_ = &UpdateService{
			Op: CreateDB,
			Instance: ServiceInstance{
				DatabaseID: proto.DatabaseID("db1"),
				Peers: &kayak.Peers{
					Term: uint64(1),
					Leader: &kayak.Server{
						Role: proto.Leader,
						ID:   proto.NodeID("node3"),
					},
					Servers: []*kayak.Server{
						{
							Role: proto.Leader,
							ID:   proto.NodeID("node3"),
						},
						{
							Role: proto.Follower,
							ID:   proto.NodeID("node2"),
						},
					},
					PubKey:    nil,
					Signature: nil,
				},
				GenesisBlock: nil,
			},
		}
		_ = &UpdateServiceResponse{}
	})
}
