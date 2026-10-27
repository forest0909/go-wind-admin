package data

import (
	"database/sql"
	"errors"
	"fmt"

	"go-wind-admin/app/admin/service/internal/data/ent"
)

// ErrTxCommit 表示事务提交失败。业务错误不要和它混在一起。
var ErrTxCommit = errors.New("transaction commit failed")

// RunInTx 执行 fn。fn 返回 nil 时在成功路径上显式 Commit。
// defer 只回滚还没提交的事务；panic 时先回滚，再把 panic 抛出去。
// Commit 失败时返回的错误可用 errors.Is(err, ErrTxCommit) 识别，并且仍会尝试回滚。
func RunInTx(tx *ent.Tx, fn func() error, onRollbackErr func(error)) error {
	if tx == nil {
		return errors.New("transaction is nil")
	}
	var committed bool
	defer finishEntTx(tx, &committed, onRollbackErr)
	if err := fn(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: %s", ErrTxCommit, err.Error())
	}
	committed = true
	return nil
}

// commitEntTx 在成功路径上提交事务。成功后把 committed 置位，defer 就不再回滚。
func commitEntTx(tx *ent.Tx, committed *bool) error {
	if tx == nil {
		return errors.New("transaction is nil")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if committed != nil {
		*committed = true
	}
	return nil
}

func finishEntTx(tx *ent.Tx, committed *bool, onRollbackErr func(error)) {
	if tx == nil {
		return
	}
	if v := recover(); v != nil {
		reportRollback(tx.Rollback(), onRollbackErr)
		panic(v)
	}
	if committed != nil && *committed {
		return
	}
	reportRollback(tx.Rollback(), onRollbackErr)
}

func reportRollback(err error, onRollbackErr func(error)) {
	if err == nil || onRollbackErr == nil || errors.Is(err, sql.ErrTxDone) {
		return
	}
	onRollbackErr(err)
}
