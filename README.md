# x402-facilitator

**x402-facilitator** is a Go-based middleware that settles on-chain payments authorized via the [x402 protocol](https://x402.dev).

## Prerequisites
- Golang 1.24 or later
- Docker
- Docker Compose

## Supported schemes × networks

x402 v2 treats the payment **scheme** (the on-chain protocol used to move
funds) and the **network** (which chain that protocol runs on) as two
independent axes. This facilitator currently supports:

| Scheme  | `eip155:*` (EVM) | `solana:*` | `sui:*` | `tron:*` | `casper:*` | `nano:*` |
|---------|:----------------:|:----------:|:-------:|:--------:|:----------:|:--------:|
| `exact` |        ✅        |     ✅     |   🚧    |    🚧    |     ✅     |    ✅     |

Networks are specified in [CAIP-2](https://chainagnostic.org/CAIPs/caip-2)
format (e.g. `eip155:84532` for Base Sepolia, `eip155:8453` for Base
mainnet, `eip155:42161` for Arbitrum One). The `exact` scheme supports
both EIP-3009 `transferWithAuthorization` and Permit2
`PermitWitnessTransferFrom` payloads on EVM chains; see the `--method`
flag on `x402-client` to pick between them.

### Casper

Casper is addressed as `casper:casper` (mainnet) and `casper:casper-test`
(testnet). Settlement uses wCSPR, a CEP-18 token with 9 decimals, so
amounts are integer motes encoded as decimal strings; conversions that
would lose sub-mote precision fail instead of truncating.

Casper payments are authorized by the payer and broadcast by a Casper
facilitator service, so `url` points at that service rather than a node
RPC endpoint. It defaults to `https://x402-facilitator.cspr.cloud` and can
be overridden through `url` in `config.toml` or the
`CASPER_FACILITATOR_URL` environment variable.

### Nano

Nano is addressed as `nano:mainnet`. It is a feeless DAG ledger with no
smart contracts and no memo field, so the payer broadcasts its own signed
send block directly and there is no facilitator settlement to broadcast or
gas token to carry. The facilitator is read-only on the chain: it verifies
the referenced send block (in `payload.blockHash`) on at least two
independent public RPC nodes (default `https://rpc.nano.to` and
`https://rainstorm.city/api`), failing closed if any node errors or the
block is unconfirmed, not a send, or its destination/amount do not match
the requirements. Settlement re-verifies then binds the block hash exactly
once. Because Nano has no memo, the binding to a specific invoice is the
pay-to destination (a per-invoice Nano account) plus the exact raw amount,
both read back from the block. Amounts are raw atomic units (1 XNO = 1e30
raw) encoded as decimal strings.

### Solana

Solana networks are `solana:mainnet`, `solana:devnet`, and other
`solana:*` CAIP-2 identifiers; `url` is the Solana JSON-RPC endpoint (for
example `https://api.devnet.solana.com`). The configured private key is
the facilitator's fee payer, advertised to payers through the
`feePayer` extra and the `signers` map of the `/supported` response.

Settlement uses SPL Token `TransferChecked`: the payer builds and signs a
base64-encoded legacy transaction carrying a single `TransferChecked`
instruction that moves `amount` base units of the `asset` mint (a base58
public key) to the associated token account of `payTo`, naming the
facilitator's fee payer as the transaction fee payer. The facilitator
verifies the transfer against the payment requirements, co-signs as fee
payer, and submits the transaction.

The supported subset is intentionally minimal; transactions outside it are
rejected:

- legacy transaction version only (no versioned/v0 transactions)
- exactly one instruction, which must be the SPL Token `TransferChecked`
  payment (no ComputeBudget, Memo, or other extra instructions)
- exactly two required signers: the facilitator fee payer and the payer,
  whose Ed25519 signature over the serialized message is verified; no SPL
  multisig or Token-2022 support
- the fee payer key must not appear as an instruction account; the
  facilitator only ever signs to pay fees
- settlement reports success only after the RPC reports the transaction
  confirmed and successful; on-chain failures and confirmation timeouts
  are structured settlement failures

### Sui

`scheme/sui` resolves Sui gasless-stablecoin payments: `PreparePayment`
returns the unsigned consolidation and payment transactions a payer signs
with their own Sui runtime, and the stablecoin amount conversion helpers
(`StablecoinAmountToAtomic` / `FormatStablecoinAtomicAmount`) convert
between human amounts and atomic units using the decimals registered per
network. `scheme/sui/http` (package `suihttp`) is the Sui x402 prepare
HTTP handler and browser client for that flow: `NewPrepareHandler` serves
one fixed contract, while applications routing a shared prepare endpoint
over multiple paid contracts build a `Preparer` per contract
(`NewPreparer`) and call `WritePrepare` after their own route selection.

## Resource server (HTTP integration)

`resource/http` (package `x402http`) is a chain-blind, application-blind
payment gate for `net/http` services. The gate owns the x402 wire surface:
it parses the inbound `PAYMENT-SIGNATURE` header (canonical v2; legacy
`X-PAYMENT` accepted as a fallback), answers unpaid and undecodable requests
with a `402` challenge carrying the resource metadata and accepted
requirements, settles paid requests through a `Facilitator` **before** the
resource handler runs, publishes the base64 settlement receipt in
`PAYMENT-RESPONSE` / `X-PAYMENT-RESPONSE` headers, and strips every payment
header from the forwarded request. Which routes or HTTP methods are paid is
application policy: wrap exactly the handlers that should be paid.

Phase 1 implements the canonical **upfront** flow only: the gate settles
before the resource handler runs and normalizes the accepted requirements to
`extra.paymentFlow = "upfront"` — without it, clients would misread the
requirements as the default `authorization` flow — and rejects a configured
`extra.paymentFlow` that conflicts with `upfront`. The remaining canonical
flows (`authorization`, `escrow`) are future phases.

The gate also owns payment cache policy: 402 challenges and settlement
failures are `Cache-Control: no-store`, and any response carrying a
settlement receipt gains the `private` directive so a shared proxy or CDN
cannot serve a paid response without the gate running. A structured
settlement failure (`Success: false`) still carries its `PAYMENT-RESPONSE`
receipt on the 402, so a client can tell a pending broadcast from a payable
failure instead of paying blindly again.

```go
import (
	"net/http"

	x402http "github.com/gosuda/x402-facilitator/resource/http"
	evmfacilitator "github.com/gosuda/x402-facilitator/scheme/evm/facilitator"
	"github.com/gosuda/x402-facilitator/types"
)

// Chain facilitators are composed explicitly. The EVM one lives in the
// scheme/evm module (separate go.mod); the Solana/Sui/Tron/Casper ones live
// under scheme/<chain>/facilitator in the root module.
fac, err := evmfacilitator.NewEVMFacilitator(
	"eip155:84532", "https://sepolia.base.org", privateKeyHex)
if err != nil {
	return err
}

gate, err := x402http.New(x402http.Config{
	Requirements: types.PaymentRequirements{
		Scheme:  string(types.Exact),
		Network: "eip155:84532",
		Asset:   "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		Amount:  "10000",
		PayTo:   "0xYourReceivingAddress",
	},
	Facilitator: fac,
	Resource: &types.ResourceInfo{
		URL: "https://your.example/paid-resource",
	},
})
if err != nil {
	return err
}

mux := http.NewServeMux()
mux.Handle("/paid-resource", gate.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// receipt headers are already set; the settlement is also on the context
	settlement, _ := x402http.SettlementFrom(r.Context())
	w.Write([]byte("paid: " + settlement.Transaction))
})))
```

Anything satisfying the gate's two-method `Facilitator` interface works as
the settlement backend: the chain facilitators in this repository (EVM from
`scheme/evm/facilitator`; Solana, Sui, Tron, Casper from
`scheme/<chain>/facilitator`) and the remote `api/client.Client` for talking
to a separate facilitator deployment.

## Module layout

The repository is split into three Go modules with one-directional
dependencies, so a Sui-only or remote-facilitator consumer of the root
module no longer inherits the EVM/go-ethereum graph (the root module still
contains the Solana and Sui SDKs; isolating those, if ever needed, would
follow the same pattern):

| Module (directory) | Contents | Pulls |
|---|---|---|
| `github.com/gosuda/x402-facilitator` (root) | `types`, `resource/http`, `api`, `scheme` (facilitator contract + Solana/Sui/Tron/Casper facilitators), `utils` | no go-ethereum, no x402 SDK |
| `github.com/gosuda/x402-facilitator/scheme/evm` (`scheme/evm/`) | EVM scheme, EIP-3009/Permit2, EVM facilitator, SDK wire-compat guard | go-ethereum, x402-foundation |
| `github.com/gosuda/x402-facilitator/cmd` (`cmd/`) | `x402-facilitator` + `x402-client` binaries and the chain registry | root + scheme/evm |

Dependencies are one-directional: `scheme/evm` requires the root module,
and `cmd` requires both — the root module never requires the EVM module,
so a Sui-only or remote-facilitator consumer imports the root module and
inherits no go-ethereum graph. `scheme.Facilitator` is the common
contract every scheme implements; each concrete facilitator lives beside
its chain (`scheme/<chain>/facilitator`), and the shipped
`x402-facilitator` binary composes them explicitly in
`cmd/facilitator/registry.go` — which chains a distribution includes is
a distribution choice, not a library property. Libraries that need only
some chains construct the `scheme/<chain>/facilitator` types they want
directly.

## How to run

### Build binary
```bash
make build
```

### Run x402-facilitator

#### 1. Run with docker compose
```bash
docker compose up
```

#### 2. Configuration
x402-facilitator is configured via `config.toml`.
```toml
# Port for HTTP server (default: 9090)
port = 9090

# Payment protocol scheme. Currently only "exact" is supported; the
# value is the x402 v2 scheme identifier, not a chain name.
scheme = "exact"

# Network in CAIP-2 format. Examples:
#   eip155:84532  — Base Sepolia
#   eip155:8453   — Base mainnet
#   eip155:42161  — Arbitrum One
network = "eip155:84532"

# RPC endpoint the facilitator uses to verify and broadcast
# transactions on the configured network.
url = "https://sepolia.base.org"

# Private key of the facilitator's fee payer (hex, no 0x prefix).
# Leave empty in the repo; inject via your deployment's secret
# management.
privateKey = ""
```

#### 3. Api Specification
After starting the service, open your browser to:
```
/swagger/index.html
```

### Run x402-client
```
Usage:
  x402-client [flags]

Flags:
  -A, --amount string    Amount to send
  -F, --from string      Sender address
  -h, --help             help for x402-client
  -m, --method string    Payment method (eip3009 or permit2) (default "eip3009")
  -n, --network string   CAIP-2 network to pay on (default "eip155:84532")
  -P, --privkey string   Sender private key
  -s, --scheme string    Payment scheme to use (default "exact")
  -T, --to string        Recipient address
  -t, --token string     token contract for sending (default "USDC")
  -u, --url string       Base URL of the facilitator server (default "http://localhost:9090")

Example:
  x402-client -n eip155:84532 -s exact -t USDC -F {0xYourSenderAddress} -T {0xRecipientAddress} -P {YourPrivateKey} -A 1000
```


## Contributing
We welcome any contributions! Feel free to open issues or submit pull requests at any time.
