package database

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL SQLSTATE 约束冲突码。
const (
	CodeUniqueViolation     = "23505"
	CodeForeignKeyViolation = "23503"
	CodeCheckViolation      = "23514"
)

// ConstraintViolation 描述一次约束冲突：SQLSTATE 码与触发的约束名。
//
// 约束名来自迁移里的显式命名，业务层据此把「哪个唯一键冲突了」翻译成具体错误，
// 而不是笼统的「已存在」。
type ConstraintViolation struct {
	Code       string
	Constraint string
}

// AsConstraintViolation 从错误链中提取 PostgreSQL 的唯一、外键或检查约束冲突。
//
// 优先识别 pgx 的结构化错误；若驱动错误被转成了纯文本（极少见），回退到匹配 SQLSTATE 码，
// 此时约束名为空。
func AsConstraintViolation(err error) (*ConstraintViolation, bool) {
	if err == nil {
		return nil, false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case CodeUniqueViolation, CodeForeignKeyViolation, CodeCheckViolation:
			return &ConstraintViolation{Code: pgErr.Code, Constraint: pgErr.ConstraintName}, true
		}
		return nil, false
	}
	msg := err.Error()
	for _, code := range []string{CodeUniqueViolation, CodeForeignKeyViolation, CodeCheckViolation} {
		if strings.Contains(msg, "SQLSTATE "+code) || strings.Contains(msg, "("+code+")") {
			return &ConstraintViolation{Code: code}, true
		}
	}
	return nil, false
}

// IsUniqueViolation 报告 err 是否为唯一约束冲突。
func IsUniqueViolation(err error) bool {
	v, ok := AsConstraintViolation(err)
	return ok && v.Code == CodeUniqueViolation
}
