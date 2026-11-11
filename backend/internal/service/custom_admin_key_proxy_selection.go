package service

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// NormalizeAccountProxyMode 显式模式与旧 proxy_id/random_proxy 互斥，省略时保留原绑定语义。
func NormalizeAccountProxyMode(mode string, id *int64, random bool) (string, error) {
	if id != nil && *id < 0 {
		return "", infraerrors.BadRequest("PROXY_SELECTION_INVALID", "代理 ID 无效")
	}
	if random {
		if mode != "" && mode != "random" {
			return "", infraerrors.BadRequest("PROXY_SELECTION_CONFLICT", "代理选择参数冲突")
		}
		mode = "random"
	}
	switch mode {
	case "":
		return "", nil
	case "specified":
		if id == nil || *id <= 0 {
			return "", infraerrors.BadRequest("PROXY_SELECTION_INVALID", "指定代理须提供正数 proxy_id")
		}
	case "direct":
		if id != nil && *id != 0 {
			return "", infraerrors.BadRequest("PROXY_SELECTION_CONFLICT", "直连模式不能同时指定代理")
		}
	case "random", "least_used":
		if id != nil && *id != 0 {
			return "", infraerrors.BadRequest("PROXY_SELECTION_CONFLICT", "自动选择不能同时指定代理")
		}
	default:
		return "", infraerrors.BadRequest("PROXY_SELECTION_INVALID", "代理模式仅支持 direct、specified、random、least_used")
	}
	return mode, nil
}

func (s *adminServiceImpl) scopedAccountProxySelection(ctx context.Context, mode string, id *int64) (*int64, error) {
	normalized, err := NormalizeAccountProxyMode(mode, id, false)
	if err != nil {
		return nil, err
	}
	switch normalized {
	case "", "specified":
		return id, nil
	case "direct":
		zero := int64(0)
		return &zero, nil
	}
	if dbent.TxFromContext(ctx) == nil {
		return nil, fmt.Errorf("自动代理选择需要账户写事务")
	}
	repo, ok := s.proxyRepo.(interface {
		SelectAccountProxy(context.Context, string) (int64, error)
	})
	if !ok {
		return nil, fmt.Errorf("自动代理选择服务不可用")
	}
	selected, err := repo.SelectAccountProxy(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if selected <= 0 {
		return nil, infraerrors.BadRequest("PROXY_POOL_EMPTY", "没有启用且未过期的代理")
	}
	return &selected, nil
}

// prepareCreateAccountProxy 把创建请求里的显式代理模式解析成具体 proxy_id。
// 自动模式必须在账户写事务内选择，避免 least_used 在提交前被并发请求穿透；
// 没有事务时返回 needsTx=true，由调用方开启事务后重新进入创建流程。
func (s *adminServiceImpl) prepareCreateAccountProxy(ctx context.Context, input *CreateAccountInput) (needsTx bool, err error) {
	normalized, err := NormalizeAccountProxyMode(input.ProxyMode, input.ProxyID, false)
	if err != nil {
		return false, err
	}
	switch normalized {
	case "":
		return false, nil
	case "specified":
		input.ProxyMode = ""
		return false, nil
	case "direct":
		// 创建以 NULL 表示直连。
		input.ProxyMode = ""
		input.ProxyID = nil
		return false, nil
	}
	if dbent.TxFromContext(ctx) == nil {
		return true, nil
	}
	selected, err := s.scopedAccountProxySelection(ctx, normalized, input.ProxyID)
	if err != nil {
		return false, err
	}
	input.ProxyID = selected
	input.ProxyMode = ""
	return false, nil
}

// prepareUpdateAccountProxy 与创建一致，但直连使用 0 作为“清除既有绑定”的指令。
func (s *adminServiceImpl) prepareUpdateAccountProxy(ctx context.Context, input *UpdateAccountInput) (needsTx bool, err error) {
	normalized, err := NormalizeAccountProxyMode(input.ProxyMode, input.ProxyID, false)
	if err != nil {
		return false, err
	}
	switch normalized {
	case "":
		return false, nil
	case "specified":
		input.ProxyMode = ""
		return false, nil
	case "direct":
		zero := int64(0)
		input.ProxyID = &zero
		input.ProxyMode = ""
		return false, nil
	}
	if dbent.TxFromContext(ctx) == nil {
		return true, nil
	}
	selected, err := s.scopedAccountProxySelection(ctx, normalized, input.ProxyID)
	if err != nil {
		return false, err
	}
	input.ProxyID = selected
	input.ProxyMode = ""
	return false, nil
}

// createAccountWithAutoProxyTx 在一个事务内完成代理选择、账户创建和分组绑定，
// 没有外层事务时由 CreateAccount 自动开启。
func (s *adminServiceImpl) createAccountWithAutoProxyTx(ctx context.Context, input *CreateAccountInput) (*Account, error) {
	if s.entClient == nil {
		return nil, fmt.Errorf("自动代理选择需要数据库事务")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txctx := dbent.NewTxContext(ctx, tx)
	safe := *input
	account, err := s.CreateAccount(txctx, &safe)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	input.ProxyID = safe.ProxyID
	input.ProxyMode = ""
	return account, nil
}

// updateAccountWithAutoProxyTx 保证自动选择与账户更新一同提交或回滚。
func (s *adminServiceImpl) updateAccountWithAutoProxyTx(ctx context.Context, id int64, input *UpdateAccountInput) (*Account, error) {
	if s.entClient == nil {
		return nil, fmt.Errorf("自动代理选择需要数据库事务")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txctx := dbent.NewTxContext(ctx, tx)
	safe := *input
	account, err := s.UpdateAccount(txctx, id, &safe)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	input.ProxyID = safe.ProxyID
	input.ProxyMode = ""
	return account, nil
}
