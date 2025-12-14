package ledger

import (
    context "context"
    grpc "google.golang.org/grpc"
)

type LedgerServiceClient interface {
    CreateAccount(ctx context.Context, in *CreateAccountRequest, opts ...grpc.CallOption) (*CreateAccountResponse, error)
    Credit(ctx context.Context, in *CreditRequest, opts ...grpc.CallOption) (*CreditResponse, error)
    Debit(ctx context.Context, in *DebitRequest, opts ...grpc.CallOption) (*DebitResponse, error)
    Transfer(ctx context.Context, in *TransferRequest, opts ...grpc.CallOption) (*TransferResponse, error)
    GetBalance(ctx context.Context, in *GetBalanceRequest, opts ...grpc.CallOption) (*GetBalanceResponse, error)
    Reconcile(ctx context.Context, in *ReconcileRequest, opts ...grpc.CallOption) (*ReconcileResponse, error)
    ListAccounts(ctx context.Context, in *ListAccountsRequest, opts ...grpc.CallOption) (*ListAccountsResponse, error)
    GetAccount(ctx context.Context, in *GetAccountRequest, opts ...grpc.CallOption) (*GetAccountResponse, error)
    ValidateConsistency(ctx context.Context, in *ValidateConsistencyRequest, opts ...grpc.CallOption) (*ValidateConsistencyResponse, error)
}

type ledgerServiceClient struct {
    cc grpc.ClientConnInterface
}

func NewLedgerServiceClient(cc grpc.ClientConnInterface) LedgerServiceClient {
    return &ledgerServiceClient{cc: cc}
}

func (c *ledgerServiceClient) CreateAccount(ctx context.Context, in *CreateAccountRequest, opts ...grpc.CallOption) (*CreateAccountResponse, error) {
    out := new(CreateAccountResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/CreateAccount", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) Credit(ctx context.Context, in *CreditRequest, opts ...grpc.CallOption) (*CreditResponse, error) {
    out := new(CreditResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/Credit", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) Debit(ctx context.Context, in *DebitRequest, opts ...grpc.CallOption) (*DebitResponse, error) {
    out := new(DebitResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/Debit", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) Transfer(ctx context.Context, in *TransferRequest, opts ...grpc.CallOption) (*TransferResponse, error) {
    out := new(TransferResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/Transfer", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) GetBalance(ctx context.Context, in *GetBalanceRequest, opts ...grpc.CallOption) (*GetBalanceResponse, error) {
    out := new(GetBalanceResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/GetBalance", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) Reconcile(ctx context.Context, in *ReconcileRequest, opts ...grpc.CallOption) (*ReconcileResponse, error) {
    out := new(ReconcileResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/Reconcile", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) ListAccounts(ctx context.Context, in *ListAccountsRequest, opts ...grpc.CallOption) (*ListAccountsResponse, error) {
    out := new(ListAccountsResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/ListAccounts", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) GetAccount(ctx context.Context, in *GetAccountRequest, opts ...grpc.CallOption) (*GetAccountResponse, error) {
    out := new(GetAccountResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/GetAccount", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

func (c *ledgerServiceClient) ValidateConsistency(ctx context.Context, in *ValidateConsistencyRequest, opts ...grpc.CallOption) (*ValidateConsistencyResponse, error) {
    out := new(ValidateConsistencyResponse)
    err := c.cc.Invoke(ctx, "/ledger.LedgerService/ValidateConsistency", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}

type LedgerServiceServer interface {
    CreateAccount(context.Context, *CreateAccountRequest) (*CreateAccountResponse, error)
    Credit(context.Context, *CreditRequest) (*CreditResponse, error)
    Debit(context.Context, *DebitRequest) (*DebitResponse, error)
    Transfer(context.Context, *TransferRequest) (*TransferResponse, error)
    GetBalance(context.Context, *GetBalanceRequest) (*GetBalanceResponse, error)
    Reconcile(context.Context, *ReconcileRequest) (*ReconcileResponse, error)
    ListAccounts(context.Context, *ListAccountsRequest) (*ListAccountsResponse, error)
    GetAccount(context.Context, *GetAccountRequest) (*GetAccountResponse, error)
    ValidateConsistency(context.Context, *ValidateConsistencyRequest) (*ValidateConsistencyResponse, error)
    MustEmbedUnimplementedLedgerServiceServer()
}

type UnimplementedLedgerServiceServer struct{}

func (UnimplementedLedgerServiceServer) CreateAccount(context.Context, *CreateAccountRequest) (*CreateAccountResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) Credit(context.Context, *CreditRequest) (*CreditResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) Debit(context.Context, *DebitRequest) (*DebitResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) Transfer(context.Context, *TransferRequest) (*TransferResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) GetBalance(context.Context, *GetBalanceRequest) (*GetBalanceResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) Reconcile(context.Context, *ReconcileRequest) (*ReconcileResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) ListAccounts(context.Context, *ListAccountsRequest) (*ListAccountsResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) GetAccount(context.Context, *GetAccountRequest) (*GetAccountResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) ValidateConsistency(context.Context, *ValidateConsistencyRequest) (*ValidateConsistencyResponse, error) {
    return nil, nil
}
func (UnimplementedLedgerServiceServer) MustEmbedUnimplementedLedgerServiceServer() {}

type UnsafeLedgerServiceServer interface {
    MustEmbedUnimplementedLedgerServiceServer()
}

func RegisterLedgerServiceServer(s grpc.ServiceRegistrar, srv LedgerServiceServer) {
    _ = srv
    _ = s
}