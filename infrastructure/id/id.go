// Package id —— authn.IDGenerator 端口实现（雪花 ID）。
package id

import "github.com/bwmarrin/snowflake"

// Generator 雪花发生器。
type Generator struct{ node *snowflake.Node }

// NewSnowflake 构造（nodeID 默认 1）。
func NewSnowflake(nodeID int64) (*Generator, error) {
	n, err := snowflake.NewNode(nodeID)
	if err != nil {
		return nil, err
	}
	return &Generator{node: n}, nil
}

// New 生成全局唯一 ID。
func (g *Generator) New() int64 { return g.node.Generate().Int64() }
