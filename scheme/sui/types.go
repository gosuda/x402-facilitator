package sui

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/btcsuite/btcutil/base58"
	bcs "github.com/iotaledger/bcs-go"
	"golang.org/x/crypto/blake2b"
)

const (
	TransactionKindProgrammable = "ProgrammableTransaction"

	transactionDataDigestTag = "TransactionData::"
	addressLength            = 32
	digestLength             = 32
	suiCoinObjectPageLimit   = 50

	CommandKindMoveCall        = "MoveCall"
	CommandKindTransferObjects = "TransferObjects"
	CommandKindSplitCoins      = "SplitCoins"
	CommandKindMergeCoins      = "MergeCoins"
	CommandKindPublish         = "Publish"
	CommandKindMakeMoveVec     = "MakeMoveVec"
	CommandKindUpgrade         = "Upgrade"
)

func init() {
	bcs.AddCustomEncoder(func(e *bcs.Encoder, d Digest) error {
		if len(d) != digestLength {
			return fmt.Errorf("digest must be %d bytes, got %d", digestLength, len(d))
		}
		e.WriteLen(len(d))
		_, err := e.Write(d)
		return err
	})
	bcs.AddCustomDecoder(func(d *bcs.Decoder, digest *Digest) error {
		length := d.ReadLen()
		if err := d.Err(); err != nil {
			return err
		}
		if length != digestLength {
			return fmt.Errorf("digest must be %d bytes, got %d", digestLength, length)
		}
		raw, err := d.ReadN(length)
		if err != nil {
			return err
		}
		*digest = append((*digest)[:0], raw...)
		return nil
	})
}

type Address [addressLength]byte

func ParseAddress(value string) (Address, error) {
	normalized := NormalizeAddress(value)
	if normalized == "" {
		return Address{}, fmt.Errorf("invalid Sui address: %s", value)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(normalized, "0x"))
	if err != nil {
		return Address{}, err
	}
	if len(decoded) != addressLength {
		return Address{}, fmt.Errorf("invalid Sui address length: %d", len(decoded))
	}
	var address Address
	copy(address[:], decoded)
	return address, nil
}

func (a Address) String() string {
	return "0x" + hex.EncodeToString(a[:])
}

func (a Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

func (a *Address) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*a = Address{}
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	address, err := ParseAddress(value)
	if err != nil {
		return err
	}
	*a = address
	return nil
}

type Digest []byte

func ParseDigest(value string) (Digest, error) {
	return parseDigest(value)
}

func (d Digest) String() string {
	return base58.Encode(d)
}

func (d Digest) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Digest) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	digest, err := parseDigest(value)
	if err != nil {
		return err
	}
	*d = digest
	return nil
}

type ObjectRef struct {
	ObjectID Address `json:"objectId"`
	Version  uint64  `json:"version"`
	Digest   Digest  `json:"digest"`
}

type SharedObjectRef struct {
	ObjectID             Address `json:"objectId"`
	InitialSharedVersion uint64  `json:"initialSharedVersion"`
	Mutable              bool    `json:"mutable"`
}

type TransactionBlockData struct {
	MessageVersion string           `json:"messageVersion,omitempty"`
	Transaction    *TransactionKind `json:"transaction,omitempty"`
	Sender         *Address         `json:"sender,omitempty"`
	GasData        *GasData         `json:"gasData,omitempty"`
	Expiration     json.RawMessage  `json:"expiration,omitempty"`
}

type TransactionEffects struct {
	MessageVersion     string                      `json:"messageVersion,omitempty"`
	Status             *TransactionExecutionStatus `json:"status,omitempty"`
	ExecutedEpoch      string                      `json:"executedEpoch,omitempty"`
	GasUsed            *GasUsedResult              `json:"gasUsed,omitempty"`
	ModifiedAtVersions []ModifiedAtVersion         `json:"modifiedAtVersions,omitempty"`
	TransactionDigest  Digest                      `json:"transactionDigest,omitempty"`
	Created            []ObjectOwnerResult         `json:"created,omitempty"`
	Mutated            []ObjectOwnerResult         `json:"mutated,omitempty"`
	Deleted            []ObjectRefResult           `json:"deleted,omitempty"`
	Unwrapped          []ObjectOwnerResult         `json:"unwrapped,omitempty"`
	Wrapped            []ObjectRefResult           `json:"wrapped,omitempty"`
	GasObject          *ObjectOwnerResult          `json:"gasObject,omitempty"`
	Dependencies       []Digest                    `json:"dependencies,omitempty"`
}

type TransactionExecutionStatus struct {
	Status string  `json:"status"`
	Error  *string `json:"error,omitempty"`
}

type GasUsedResult struct {
	ComputationCost         string `json:"computationCost"`
	StorageCost             string `json:"storageCost"`
	StorageRebate           string `json:"storageRebate"`
	NonRefundableStorageFee string `json:"nonRefundableStorageFee"`
}

type ModifiedAtVersion struct {
	ObjectID       Address `json:"objectId"`
	SequenceNumber string  `json:"sequenceNumber"`
}

type BalanceChange struct {
	Owner    interface{} `json:"owner"`
	CoinType string      `json:"coinType"`
	Amount   string      `json:"amount"`
}

type ObjectChange struct {
	Type            string      `json:"type"`
	Sender          *Address    `json:"sender,omitempty"`
	Owner           interface{} `json:"owner,omitempty"`
	ObjectType      string      `json:"objectType,omitempty"`
	ObjectID        *Address    `json:"objectId,omitempty"`
	Version         string      `json:"version,omitempty"`
	PreviousVersion string      `json:"previousVersion,omitempty"`
	Digest          *Digest     `json:"digest,omitempty"`
	PackageID       *Address    `json:"packageId,omitempty"`
	Modules         []string    `json:"modules,omitempty"`
}

type ObjectOwnerResult struct {
	Owner     interface{}      `json:"owner"`
	Reference *ObjectRefResult `json:"reference,omitempty"`
}

type ObjectRefResult struct {
	ObjectID Address       `json:"objectId"`
	Version  ObjectVersion `json:"version"`
	Digest   Digest        `json:"digest"`
}

type ObjectVersion string

func (v ObjectVersion) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(v))
}

func (v *ObjectVersion) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*v = ""
		return nil
	}

	var quoted string
	if err := json.Unmarshal(data, &quoted); err == nil {
		quoted = strings.TrimSpace(quoted)
		if quoted != "" {
			if _, err := strconv.ParseUint(quoted, 10, 64); err != nil {
				return fmt.Errorf("invalid object version %q: %w", quoted, err)
			}
		}
		*v = ObjectVersion(quoted)
		return nil
	}

	if _, err := strconv.ParseUint(raw, 10, 64); err != nil {
		return fmt.Errorf("invalid object version %q: %w", raw, err)
	}
	*v = ObjectVersion(raw)
	return nil
}

type GasData struct {
	Payment []ObjectRefResult `json:"payment,omitempty"`
	Owner   *Address          `json:"owner,omitempty"`
	Price   string            `json:"price,omitempty"`
	Budget  string            `json:"budget,omitempty"`
}

type TransactionKind struct {
	Kind         string            `json:"kind,omitempty"`
	Inputs       []json.RawMessage `json:"inputs,omitempty"`
	Transactions []json.RawMessage `json:"transactions,omitempty"`
	Commands     []json.RawMessage `json:"commands,omitempty"`
}

type ExecuteTransactionBlock struct {
	Digest                  Digest              `json:"digest"`
	Effects                 *TransactionEffects `json:"effects,omitempty"`
	ObjectChanges           []ObjectChange      `json:"objectChanges,omitempty"`
	BalanceChanges          []BalanceChange     `json:"balanceChanges,omitempty"`
	TimestampMs             *string             `json:"timestampMs,omitempty"`
	ConfirmedLocalExecution bool                `json:"confirmedLocalExecution,omitempty"`
	Checkpoint              *string             `json:"checkpoint,omitempty"`
	Errors                  []string            `json:"errors,omitempty"`
}

func (r *ExecuteTransactionBlock) IsSuccess() bool {
	return r != nil && transactionEffectsSuccess(r.Effects)
}

func (r *ExecuteTransactionBlock) GetError() *string {
	if r == nil || r.Effects == nil || r.Effects.Status == nil {
		return nil
	}
	return r.Effects.Status.Error
}

type MoveCallCommand struct {
	Target        string            `json:"target,omitempty"`
	Package       string            `json:"package,omitempty"`
	Module        string            `json:"module,omitempty"`
	Function      string            `json:"function,omitempty"`
	TypeArguments []string          `json:"type_arguments,omitempty"`
	Arguments     []json.RawMessage `json:"arguments,omitempty"`
}

type GaslessStablecoinTransfer struct {
	Sender    string
	Recipient string
	Network   string
	Asset     string
	Amount    string
	Endpoints []string

	// Expiration can be provided to build fully offline. If nil, the builder
	// resolves a ValidDuring expiration from the network using Endpoints plus
	// configured default endpoints.
	Expiration *TransactionExpiration
}

type OwnedCoinObject struct {
	ObjectRef           ObjectRef `json:"objectRef"`
	CoinType            string    `json:"coinType,omitempty"`
	Balance             uint64    `json:"balance"`
	PreviousTransaction string    `json:"previousTransaction,omitempty"`
}

// CoinObjectsToAddressBalanceTransfer builds a PTB that deposits token balance
// into an address balance by consuming Coin<T> objects with
// 0x2::coin::send_funds<T>.

type CoinObjectsToAddressBalanceTransfer struct {
	Sender    string
	Recipient string
	Network   string
	Asset     string

	// CoinObjects are Coin<T> objects to consume in the same transaction. They
	// are sent whole.
	CoinObjects []OwnedCoinObject

	// Expiration can be provided to build fully offline. If nil, the builder
	// resolves a ValidDuring expiration from Network.
	Expiration *TransactionExpiration
	Endpoints  []string
}

type GaslessStablecoinObjectBalancePayment struct {
	Sender    string
	Recipient string
	Network   string
	Asset     string
	Amount    string
	Endpoints []string
}

type GaslessStablecoinObjectBalancePaymentResult struct {
	CoinObjects        []OwnedCoinObject
	PreparedAmount     string
	PaymentAmount      string
	PrepareTransaction *ExecuteTransactionBlock
	PaymentTransaction *ExecuteTransactionBlock
}

type TransactionExpiration struct {
	None        *struct{}
	Epoch       *uint64
	ValidDuring *ValidDuring
}

func (TransactionExpiration) IsBcsEnum() {}

type ValidDuring struct {
	MinEpoch     *uint64 `bcs:"optional"`
	MaxEpoch     *uint64 `bcs:"optional"`
	MinTimestamp *uint64 `bcs:"optional"`
	MaxTimestamp *uint64 `bcs:"optional"`
	Chain        Digest
	Nonce        uint32
}

func ParseTransactionBlockData(data []byte) (*TransactionBlockData, error) {
	var transaction TransactionBlockData
	if err := json.Unmarshal(data, &transaction); err != nil {
		return nil, err
	}
	return &transaction, nil
}

type DryRunTransactionBlock struct {
	Input          TransactionBlockData `json:"input"`
	Effects        TransactionEffects   `json:"effects"`
	ObjectChanges  []ObjectChange       `json:"objectChanges,omitempty"`
	BalanceChanges []BalanceChange      `json:"balanceChanges,omitempty"`
}

func ParseDryRunTransactionBlock(data []byte) (*DryRunTransactionBlock, error) {
	var dryRun DryRunTransactionBlock
	if err := json.Unmarshal(data, &dryRun); err != nil {
		return nil, err
	}
	return &dryRun, nil
}

func (r DryRunTransactionBlock) Success() bool {
	return transactionEffectsSuccess(&r.Effects)
}

func (r DryRunTransactionBlock) StatusError() string {
	return transactionEffectsStatusError(&r.Effects, "dry run failed")
}

func (r DryRunTransactionBlock) Gasless() bool {
	gasData := r.Input.GasData
	return gasData != nil &&
		len(gasData.Payment) == 0 &&
		gasData.Price == "0" &&
		gasData.Budget == "0"
}

func (r DryRunTransactionBlock) ValidateGaslessStablecoinPayment(asset string) error {
	if !r.Gasless() {
		return errors.New("transaction is not gasless")
	}
	if transactionEffectsHasObjectWrites(&r.Effects) || len(r.ObjectChanges) > 0 {
		return errors.New("transaction writes objects")
	}

	commands := TransactionCommands(r.Input.Transaction)
	if len(commands) == 0 {
		return errors.New("transaction has no programmable commands")
	}

	moveCallCount := 0
	for _, command := range commands {
		if command.Kind != CommandKindMoveCall || command.MoveCall == nil {
			return fmt.Errorf("unsupported programmable command %q", command.Kind)
		}
		moveCallCount++
		if err := ValidateGaslessStablecoinMoveCall(*command.MoveCall, asset); err != nil {
			return err
		}
	}
	if moveCallCount == 0 {
		return errors.New("transaction has no move calls")
	}

	return nil
}

func (r DryRunTransactionBlock) BalanceDelta(owner string, coinType string) *big.Int {
	total := new(big.Int)
	normalizedOwner := NormalizeAddress(owner)
	normalizedCoinType := NormalizeType(coinType)

	for _, change := range r.BalanceChanges {
		if OwnerAddress(change.Owner) != normalizedOwner {
			continue
		}
		if NormalizeType(change.CoinType) != normalizedCoinType {
			continue
		}

		amount, ok := new(big.Int).SetString(change.Amount, 10)
		if !ok {
			continue
		}
		total.Add(total, amount)
	}

	return total
}

func TransactionSender(txBytes []byte) (string, error) {
	if len(txBytes) == 0 {
		return "", ErrEmptyTransaction
	}
	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	if err != nil {
		return "", fmt.Errorf("invalid transaction data: %w", err)
	}
	if txData.V1 == nil {
		return "", errors.New("transaction missing V1 data")
	}
	return NormalizeAddress(txData.V1.Sender.String()), nil
}

func TransactionDigest(txBytes []byte) (string, error) {
	if len(txBytes) == 0 {
		return "", ErrEmptyTransaction
	}
	if len(txBytes) > int(^uint(0)>>1)-len(transactionDataDigestTag) {
		return "", errors.New("transaction data too large")
	}
	tagged := make([]byte, 0, len(transactionDataDigestTag)+len(txBytes))
	tagged = append(tagged, transactionDataDigestTag...)
	tagged = append(tagged, txBytes...)
	digest := blake2b.Sum256(tagged)
	return Digest(digest[:]).String(), nil
}

func ParseExecuteTransactionBlock(data []byte) (*ExecuteTransactionBlock, error) {
	var executed ExecuteTransactionBlock
	if err := json.Unmarshal(data, &executed); err != nil {
		return nil, err
	}
	return &executed, nil
}

func TransactionResultStatusError(result *ExecuteTransactionBlock, fallback string) string {
	if result == nil {
		return fallback
	}
	if err := result.GetError(); err != nil && *err != "" {
		return *err
	}
	return transactionEffectsStatusError(result.Effects, fallback)
}

func TransactionResultBalanceDelta(result *ExecuteTransactionBlock, owner string, coinType string) *big.Int {
	total := new(big.Int)
	if result == nil {
		return total
	}

	normalizedOwner := NormalizeAddress(owner)
	normalizedCoinType := NormalizeType(coinType)
	for _, change := range result.BalanceChanges {
		if OwnerAddress(change.Owner) != normalizedOwner {
			continue
		}
		if NormalizeType(change.CoinType) != normalizedCoinType {
			continue
		}

		amount, ok := new(big.Int).SetString(change.Amount, 10)
		if !ok {
			continue
		}
		total.Add(total, amount)
	}
	return total
}

func MinimumGaslessStablecoinAmount(decimals uint8) *big.Int {
	if decimals <= 2 {
		return big.NewInt(1)
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)-2), nil)
}

func TransactionExpirationValidDuring(chainDigest string, minEpoch uint64, maxEpoch uint64, nonce uint32) (*TransactionExpiration, error) {
	chain, err := parseDigest(chainDigest)
	if err != nil {
		return nil, err
	}
	return &TransactionExpiration{
		ValidDuring: &ValidDuring{
			MinEpoch: &minEpoch,
			MaxEpoch: &maxEpoch,
			Chain:    chain,
			Nonce:    nonce,
		},
	}, nil
}

func transactionEffectsSuccess(e *TransactionEffects) bool {
	return e != nil && e.Status != nil && strings.EqualFold(e.Status.Status, "success")
}

func transactionEffectsStatusError(e *TransactionEffects, fallback string) string {
	if e != nil && e.Status != nil {
		if e.Status.Error != nil && *e.Status.Error != "" {
			return *e.Status.Error
		}
		if e.Status.Status != "" {
			return e.Status.Status
		}
	}
	return fallback
}

func transactionEffectsHasObjectWrites(e *TransactionEffects) bool {
	if e == nil {
		return false
	}
	return len(e.Created) > 0 ||
		len(e.Mutated) > 0 ||
		len(e.Deleted) > 0 ||
		len(e.Wrapped) > 0 ||
		len(e.Unwrapped) > 0
}

func OwnerAddress(owner interface{}) string {
	switch value := owner.(type) {
	case nil:
		return ""
	case json.RawMessage:
		rawText := strings.TrimSpace(string(value))
		if rawText == "" || rawText == "null" {
			return ""
		}

		var tagged map[string]json.RawMessage
		if err := json.Unmarshal(value, &tagged); err == nil {
			for _, key := range []string{"AddressOwner", "ObjectOwner"} {
				if taggedValue, ok := tagged[key]; ok {
					var address string
					if err := json.Unmarshal(taggedValue, &address); err == nil {
						return NormalizeAddress(address)
					}
				}
			}
		}

		var address string
		if err := json.Unmarshal(value, &address); err == nil {
			return NormalizeAddress(address)
		}
	case string:
		return NormalizeAddress(value)
	case map[string]interface{}:
		for _, key := range []string{"AddressOwner", "ObjectOwner"} {
			if taggedValue, ok := value[key]; ok {
				if address, ok := taggedValue.(string); ok {
					return NormalizeAddress(address)
				}
			}
		}
	case map[string]json.RawMessage:
		for _, key := range []string{"AddressOwner", "ObjectOwner"} {
			if taggedValue, ok := value[key]; ok {
				var address string
				if err := json.Unmarshal(taggedValue, &address); err == nil {
					return NormalizeAddress(address)
				}
			}
		}
	}

	raw, err := json.Marshal(owner)
	if err != nil {
		return ""
	}
	return OwnerAddress(json.RawMessage(raw))
}

func validateObjectRef(name string, ref ObjectRef) error {
	if isZeroAddress(ref.ObjectID) {
		return fmt.Errorf("%s object ID is empty", name)
	}
	if len(ref.Digest) != digestLength {
		return fmt.Errorf("%s digest must be %d bytes, got %d", name, digestLength, len(ref.Digest))
	}
	return nil
}

func isZeroAddress(address Address) bool {
	var zero Address
	return address == zero
}

func parseDigest(value string) (Digest, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty digest")
	}

	decoded := base58.Decode(value)
	if len(decoded) == digestLength {
		return Digest(decoded), nil
	}

	hexValue := strings.TrimPrefix(strings.ToLower(value), "0x")
	if len(hexValue) == digestLength*2 {
		decoded, err := hex.DecodeString(hexValue)
		if err != nil {
			return nil, err
		}
		if len(decoded) == digestLength {
			return Digest(decoded), nil
		}
	}

	return nil, fmt.Errorf("digest must be a 32-byte base58 or hex value: %s", value)
}

type TypeTag struct {
	Bool    *struct{}
	U8      *struct{}
	U64     *struct{}
	U128    *struct{}
	Address *struct{}
	Signer  *struct{}
	Vector  *TypeTag
	Struct  *StructTag
	U16     *struct{}
	U32     *struct{}
	U256    *struct{}
}

func (TypeTag) IsBcsEnum() {}

func ParseTypeTag(value string) (TypeTag, error) {
	parser := typeTagParser{input: strings.TrimSpace(value)}
	tag, err := parser.parseType()
	if err != nil {
		return TypeTag{}, err
	}
	parser.skipSpaces()
	if !parser.done() {
		return TypeTag{}, fmt.Errorf("unexpected trailing type tag input at %q", parser.remaining())
	}
	return tag, nil
}

type typeTagParser struct {
	input string
	pos   int
}

func (p *typeTagParser) parseType() (TypeTag, error) {
	p.skipSpaces()
	token := p.readToken()
	if token == "" {
		return TypeTag{}, errors.New("empty type tag")
	}

	switch strings.ToLower(token) {
	case "bool":
		return TypeTag{Bool: &struct{}{}}, nil
	case "u8":
		return TypeTag{U8: &struct{}{}}, nil
	case "u16":
		return TypeTag{U16: &struct{}{}}, nil
	case "u32":
		return TypeTag{U32: &struct{}{}}, nil
	case "u64":
		return TypeTag{U64: &struct{}{}}, nil
	case "u128":
		return TypeTag{U128: &struct{}{}}, nil
	case "u256":
		return TypeTag{U256: &struct{}{}}, nil
	case "address":
		return TypeTag{Address: &struct{}{}}, nil
	case "signer":
		return TypeTag{Signer: &struct{}{}}, nil
	case "vector":
		inner, err := p.parseTypeArguments()
		if err != nil {
			return TypeTag{}, err
		}
		if len(inner) != 1 {
			return TypeTag{}, fmt.Errorf("vector expects one type argument, got %d", len(inner))
		}
		return TypeTag{Vector: &inner[0]}, nil
	}

	parts := strings.Split(token, "::")
	if len(parts) != 3 {
		return TypeTag{}, fmt.Errorf("unsupported type tag: %s", token)
	}
	address, err := ParseAddress(parts[0])
	if err != nil {
		return TypeTag{}, err
	}
	if parts[1] == "" || parts[2] == "" {
		return TypeTag{}, fmt.Errorf("invalid struct type tag: %s", token)
	}

	var typeParams []TypeTag
	p.skipSpaces()
	if p.peek() == '<' {
		typeParams, err = p.parseTypeArguments()
		if err != nil {
			return TypeTag{}, err
		}
	}

	return TypeTag{
		Struct: &StructTag{
			Address:    address,
			Module:     parts[1],
			Name:       parts[2],
			TypeParams: typeParams,
		},
	}, nil
}

func (p *typeTagParser) parseTypeArguments() ([]TypeTag, error) {
	p.skipSpaces()
	if !p.consume('<') {
		return nil, fmt.Errorf("expected type argument list at %q", p.remaining())
	}

	var arguments []TypeTag
	for {
		p.skipSpaces()
		if p.peek() == '>' {
			p.pos++
			return arguments, nil
		}

		argument, err := p.parseType()
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, argument)

		p.skipSpaces()
		switch p.peek() {
		case ',':
			p.pos++
		case '>':
			p.pos++
			return arguments, nil
		default:
			return nil, fmt.Errorf("expected ',' or '>' at %q", p.remaining())
		}
	}
}

func (p *typeTagParser) readToken() string {
	start := p.pos
	for p.pos < len(p.input) {
		switch p.input[p.pos] {
		case '<', '>', ',':
			return strings.TrimSpace(p.input[start:p.pos])
		default:
			p.pos++
		}
	}
	return strings.TrimSpace(p.input[start:p.pos])
}

func (p *typeTagParser) skipSpaces() {
	for p.pos < len(p.input) {
		switch p.input[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *typeTagParser) consume(ch byte) bool {
	if p.peek() != ch {
		return false
	}
	p.pos++
	return true
}

func (p *typeTagParser) peek() byte {
	if p.done() {
		return 0
	}
	return p.input[p.pos]
}

func (p *typeTagParser) done() bool {
	return p.pos >= len(p.input)
}

func (p *typeTagParser) remaining() string {
	if p.done() {
		return ""
	}
	return p.input[p.pos:]
}

func (t TypeTag) String() string {
	switch {
	case t.Bool != nil:
		return "bool"
	case t.U8 != nil:
		return "u8"
	case t.U16 != nil:
		return "u16"
	case t.U32 != nil:
		return "u32"
	case t.U64 != nil:
		return "u64"
	case t.U128 != nil:
		return "u128"
	case t.U256 != nil:
		return "u256"
	case t.Address != nil:
		return "address"
	case t.Signer != nil:
		return "signer"
	case t.Vector != nil:
		return fmt.Sprintf("vector<%s>", t.Vector.String())
	case t.Struct != nil:
		return t.Struct.String()
	default:
		return ""
	}
}

type StructTag struct {
	Address    Address
	Module     string
	Name       string
	TypeParams []TypeTag
}

func (s StructTag) String() string {
	base := fmt.Sprintf("%s::%s::%s", s.Address.String(), s.Module, s.Name)
	if len(s.TypeParams) == 0 {
		return base
	}

	params := make([]string, len(s.TypeParams))
	for i, param := range s.TypeParams {
		params[i] = param.String()
	}
	return fmt.Sprintf("%s<%s>", base, strings.Join(params, ", "))
}

type Pure struct {
	Bytes []byte
}

type ObjectArg struct {
	ImmOrOwnedObject *ObjectRef
	SharedObject     *SharedObjectRef
	Receiving        *ObjectRef
}

func (ObjectArg) IsBcsEnum() {}

type Argument struct {
	GasCoin      *struct{}
	Input        *uint16
	Result       *uint16
	NestedResult *NestedResult
}

func (Argument) IsBcsEnum() {}

type NestedResult struct {
	Index       uint16
	ResultIndex uint16
}

type ProgrammableMoveCall struct {
	Package       Address
	Module        string
	Function      string
	TypeArguments []TypeTag
	Arguments     []Argument
}

type TransferObjects struct {
	Objects []Argument
	Address Argument
}

type SplitCoins struct {
	Coin    Argument
	Amounts []Argument
}

type MergeCoins struct {
	Destination Argument
	Sources     []Argument
}

type Publish struct {
	Modules      [][]byte
	Dependencies []Address
}

type MakeMoveVec struct {
	Type     bcs.Option[TypeTag]
	Elements []Argument
}

type Upgrade struct {
	Modules      [][]byte
	Dependencies []Address
	Package      Address
	Ticket       Argument
}

type Command struct {
	MoveCall        *ProgrammableMoveCall
	TransferObjects *TransferObjects
	SplitCoins      *SplitCoins
	MergeCoins      *MergeCoins
	Publish         *Publish
	MakeMoveVec     *MakeMoveVec
	Upgrade         *Upgrade
}

func (Command) IsBcsEnum() {}

type gaslessStablecoinGasData struct {
	Payment []ObjectRef
	Owner   Address
	Price   uint64
	Budget  uint64
}

type gaslessStablecoinTransactionData struct {
	V1 *gaslessStablecoinTransactionDataV1
}

func (gaslessStablecoinTransactionData) IsBcsEnum() {}

type gaslessStablecoinTransactionDataV1 struct {
	Kind       gaslessStablecoinTransactionKind
	Sender     Address
	GasData    gaslessStablecoinGasData
	Expiration TransactionExpiration
}

type gaslessStablecoinTransactionKind struct {
	ProgrammableTransaction *gaslessStablecoinProgrammableTransaction
	ChangeEpoch             *struct{}
	Genesis                 *struct{}
	ConsensusCommitPrologue *struct{}
}

func (gaslessStablecoinTransactionKind) IsBcsEnum() {}

type gaslessStablecoinProgrammableTransaction struct {
	Inputs   []gaslessStablecoinCallArg
	Commands []Command
}

type gaslessStablecoinCallArg struct {
	Pure            *Pure
	Object          *ObjectArg
	FundsWithdrawal *gaslessStablecoinFundsWithdrawal
}

func (gaslessStablecoinCallArg) IsBcsEnum() {}

type gaslessStablecoinFundsWithdrawal struct {
	Reservation  gaslessStablecoinReservation
	TypeArg      gaslessStablecoinWithdrawalType
	WithdrawFrom gaslessStablecoinWithdrawFrom
}

type gaslessStablecoinReservation struct {
	MaxAmountU64 *uint64
}

func (gaslessStablecoinReservation) IsBcsEnum() {}

type gaslessStablecoinWithdrawalType struct {
	Balance *TypeTag
}

func (gaslessStablecoinWithdrawalType) IsBcsEnum() {}

type gaslessStablecoinWithdrawFrom struct {
	Sender  *struct{}
	Sponsor *struct{}
}

func (gaslessStablecoinWithdrawFrom) IsBcsEnum() {}

func ValidateGaslessStablecoinMoveCall(moveCall MoveCallCommand, asset string) error {
	if NormalizeAddress(moveCall.Package) != NormalizeAddress("0x2") {
		return fmt.Errorf("unsupported Move package %q", moveCall.Package)
	}
	if moveCall.Module != "balance" || (moveCall.Function != "redeem_funds" && moveCall.Function != "send_funds") {
		return fmt.Errorf("unsupported Move call %s::%s", moveCall.Module, moveCall.Function)
	}
	if len(moveCall.TypeArguments) == 0 {
		return fmt.Errorf("Move call %s::%s is missing stablecoin type argument", moveCall.Module, moveCall.Function)
	}

	expectedAsset := normalizeSuiType(asset)
	for _, typeArgument := range moveCall.TypeArguments {
		if normalizeSuiType(typeArgument) != expectedAsset {
			return fmt.Errorf("Move call type argument %q does not match required asset", typeArgument)
		}
	}

	return nil
}

func normalizeSuiType(value string) string {
	if tag, err := ParseTypeTag(value); err == nil {
		return NormalizeType(tag.String())
	}
	return NormalizeType(value)
}

func ParseTransactionKind(data []byte) (*TransactionKind, error) {
	var transaction TransactionKind
	if err := json.Unmarshal(data, &transaction); err != nil {
		return nil, err
	}
	return &transaction, nil
}

func TransactionCommands(transaction *TransactionKind) []TransactionCommand {
	if transaction == nil {
		return nil
	}

	rawCommands := transaction.Transactions
	if len(rawCommands) == 0 {
		rawCommands = transaction.Commands
	}

	commands := make([]TransactionCommand, 0, len(rawCommands))
	for _, raw := range rawCommands {
		command, err := ParseTransactionCommand(raw)
		if err != nil {
			commands = append(commands, TransactionCommand{
				Kind: "Invalid",
				Raw:  append(json.RawMessage(nil), raw...),
			})
			continue
		}
		commands = append(commands, *command)
	}
	return commands
}

type TransactionCommand struct {
	Kind     string
	MoveCall *MoveCallCommand
	Raw      json.RawMessage
}

func ParseTransactionCommand(data []byte) (*TransactionCommand, error) {
	var command TransactionCommand
	if err := json.Unmarshal(data, &command); err != nil {
		return nil, err
	}
	return &command, nil
}

func (c *TransactionCommand) UnmarshalJSON(data []byte) error {
	c.Raw = append(json.RawMessage(nil), data...)

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid programmable command: %w", err)
	}
	if len(object) == 0 {
		return errors.New("empty programmable command")
	}

	if payload, kind, ok := wrappedCommandPayload(object); ok {
		return c.unmarshalPayload(kind, payload)
	}
	if kind, ok := commandKind(object); ok {
		return c.unmarshalPayload(kind, data)
	}
	if len(object) == 1 {
		for kind := range object {
			c.Kind = kind
			return nil
		}
	}

	return errors.New("unknown programmable command")
}

func (c *TransactionCommand) unmarshalPayload(kind string, payload json.RawMessage) error {
	c.Kind = kind
	if kind == CommandKindMoveCall {
		command, err := unmarshalMoveCallCommand(payload)
		if err != nil {
			return fmt.Errorf("invalid MoveCall command: %w", err)
		}
		c.MoveCall = command
	}
	return nil
}

func unmarshalMoveCallCommand(data json.RawMessage) (*MoveCallCommand, error) {
	var moveCall MoveCallCommand
	_ = json.Unmarshal(data, &moveCall)

	var raw struct {
		Package            string            `json:"package"`
		PackageID          string            `json:"packageId"`
		PackageObjectID    string            `json:"packageObjectId"`
		Target             string            `json:"target"`
		Module             string            `json:"module"`
		Function           string            `json:"function"`
		TypeArgumentsSnake []string          `json:"type_arguments"`
		TypeArgumentsCamel []string          `json:"typeArguments"`
		Arguments          []json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	moveCall.Target = firstNonEmpty(moveCall.Target, raw.Target)
	moveCall.Package = firstNonEmpty(moveCall.Package, raw.Package, raw.PackageID, raw.PackageObjectID)
	moveCall.Module = firstNonEmpty(moveCall.Module, raw.Module)
	moveCall.Function = firstNonEmpty(moveCall.Function, raw.Function)
	if raw.Target != "" && (moveCall.Package == "" || moveCall.Module == "" || moveCall.Function == "") {
		parts := strings.Split(raw.Target, "::")
		if len(parts) == 3 {
			moveCall.Package = firstNonEmpty(moveCall.Package, parts[0])
			moveCall.Module = firstNonEmpty(moveCall.Module, parts[1])
			moveCall.Function = firstNonEmpty(moveCall.Function, parts[2])
		}
	}
	if len(moveCall.TypeArguments) == 0 {
		moveCall.TypeArguments = raw.TypeArgumentsSnake
	}
	if len(moveCall.TypeArguments) == 0 {
		moveCall.TypeArguments = raw.TypeArgumentsCamel
	}
	if len(moveCall.Arguments) == 0 {
		moveCall.Arguments = raw.Arguments
	}
	return &moveCall, nil
}

func wrappedCommandPayload(object map[string]json.RawMessage) (json.RawMessage, string, bool) {
	for key, payload := range object {
		if key == "kind" || key == "$kind" {
			continue
		}
		if kind, ok := canonicalCommandKind(key); ok {
			return payload, kind, true
		}
	}
	return nil, "", false
}

func commandKind(object map[string]json.RawMessage) (string, bool) {
	for _, key := range []string{"kind", "$kind"} {
		var value string
		if err := json.Unmarshal(object[key], &value); err == nil && value != "" {
			return canonicalCommandKindOrOriginal(value), true
		}
	}
	return "", false
}

func canonicalCommandKind(kind string) (string, bool) {
	switch normalizedCommandKind(kind) {
	case "movecall":
		return CommandKindMoveCall, true
	case "transferobjects":
		return CommandKindTransferObjects, true
	case "splitcoins":
		return CommandKindSplitCoins, true
	case "mergecoins":
		return CommandKindMergeCoins, true
	case "publish":
		return CommandKindPublish, true
	case "makemovevec":
		return CommandKindMakeMoveVec, true
	case "upgrade":
		return CommandKindUpgrade, true
	default:
		return "", false
	}
}

func canonicalCommandKindOrOriginal(kind string) string {
	if canonical, ok := canonicalCommandKind(kind); ok {
		return canonical
	}
	return kind
}

func normalizedCommandKind(kind string) string {
	kind = strings.TrimSpace(kind)
	kind = strings.ReplaceAll(kind, "_", "")
	kind = strings.ReplaceAll(kind, "-", "")
	return strings.ToLower(kind)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
