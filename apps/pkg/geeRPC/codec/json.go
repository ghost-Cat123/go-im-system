package codec

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
)

type JsonCodec struct {
	// 链接实例
	conn io.ReadWriteCloser
	// 缓冲write
	buf *bufio.Writer
	// 信息编码器
	dec *json.Decoder
	// 信息解码器
	enc *json.Encoder
}

var _ Codec = (*JsonCodec)(nil)

func NewJsonCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &JsonCodec{
		conn: conn,
		buf:  buf,
		dec:  json.NewDecoder(conn),
		enc:  json.NewEncoder(buf),
	}
}

func (j JsonCodec) Close() error {
	return j.conn.Close()
}

func (j JsonCodec) ReadHeader(h *Header) error {
	return j.dec.Decode(h)
}

func (j JsonCodec) ReadBody(body interface{}) error {
	return j.dec.Decode(body)
}

func (j JsonCodec) Write(h *Header, body interface{}) (err error) {
	// 最后刷新缓存 关闭消息编码器
	defer func() {
		_ = j.buf.Flush()
		if err != nil {
			_ = j.Close()
		}
	}()
	// 先编码头部信息
	if err := j.enc.Encode(h); err != nil {
		log.Println("rpc codec: json error encoding header:", err)
		return err
	}
	// 再编码主体信息
	if err := j.enc.Encode(body); err != nil {
		log.Println("rpc codec: json error encoding body:", err)
	}
	return nil
}
