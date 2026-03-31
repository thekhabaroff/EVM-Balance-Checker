package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	ini "gopkg.in/ini.v1"
)

const erc20ABI = `[
  {
    "constant": true,
    "inputs": [{"name": "_owner", "type": "address"}],
    "name": "balanceOf",
    "outputs": [{"name": "balance", "type": "uint256"}],
    "type": "function"
  },
  {
    "constant": true,
    "inputs": [],
    "name": "decimals",
    "outputs": [{"name": "", "type": "uint8"}],
    "type": "function"
  }
]`

type Network struct {
	Name        string
	RPC         string
	ChainID     int64
	NativeToken string
	Section     string
}

type Token struct {
	Key     string
	Symbol  string
	Network string
	Address common.Address
}

type Settings struct {
	RPCTimeoutSeconds int
	Retries           int
	AppendOutput      bool
}

type Config struct {
	Networks map[string]Network
	Tokens   []Token
	Settings Settings
}

func main() {
	log.SetFlags(log.LstdFlags)

	cfg, err := loadConfig("config.ini")
	if err != nil {
		log.Fatalf("config.ini load error: %v", err)
	}

	selectedNames := chooseNetworks(cfg.Networks)
	keys, err := loadKeys("keys.txt")
	if err != nil {
		log.Fatalf("keys.txt load error: %v", err)
	}
	if len(keys) == 0 {
		log.Fatal("keys.txt is empty")
	}

	flag := os.O_TRUNC | os.O_CREATE | os.O_WRONLY
	if cfg.Settings.AppendOutput {
		flag = os.O_APPEND | os.O_CREATE | os.O_WRONLY
	}

	out, err := os.OpenFile("balance.txt", flag, 0644)
	if err != nil {
		log.Fatalf("balance.txt open error: %v", err)
	}
	defer out.Close()

	for _, rawKey := range keys {
		address, normalizedKey, err := privateKeyToAddress(rawKey)
		if err != nil {
			log.Printf("skip invalid private key: %s (%v)", rawKey, err)
			continue
		}

		for _, networkName := range selectedNames {
			network := cfg.Networks[networkName]
			client, err := ethclient.Dial(network.RPC)
			if err != nil {
				log.Printf("RPC connect failed for %s: %v", network.Name, err)
				continue
			}

			checkNativeBalance(client, out, cfg.Settings, normalizedKey, address, network)
			checkERC20Balances(client, out, cfg.Settings, normalizedKey, address, network, filterTokensByNetwork(cfg.Tokens, network.Name))

			client.Close()
		}
	}
}

func loadConfig(path string) (*Config, error) {
	f, err := ini.Load(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Networks: map[string]Network{},
		Tokens:   []Token{},
		Settings: Settings{
			RPCTimeoutSeconds: 10,
			Retries:           3,
			AppendOutput:      true,
		},
	}

	for _, sectionName := range []string{"mainnets", "testnets"} {
		sec := f.Section(sectionName)
		for _, key := range sec.Keys() {
			parts := strings.Split(key.Value(), "|")
			if len(parts) != 3 {
				return nil, fmt.Errorf("invalid network format in [%s] for %s", sectionName, key.Name())
			}
			chainID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid chainId for %s: %w", key.Name(), err)
			}
			cfg.Networks[key.Name()] = Network{
				Name:        key.Name(),
				RPC:         strings.TrimSpace(parts[0]),
				ChainID:     chainID,
				NativeToken: strings.TrimSpace(parts[2]),
				Section:     sectionName,
			}
		}
	}

	for _, key := range f.Section("tokens").Keys() {
		parts := splitTokenKey(key.Name())
		if len(parts) < 2 {
			continue
		}
		networkName := strings.Join(parts[1:], " ")
		cfg.Tokens = append(cfg.Tokens, Token{
			Key:     key.Name(),
			Symbol:  parts[0],
			Network: networkName,
			Address: common.HexToAddress(strings.TrimSpace(key.Value())),
		})
	}

	settingsSec := f.Section("settings")
	cfg.Settings.RPCTimeoutSeconds = settingsSec.Key("rpc_timeout_seconds").MustInt(10)
	cfg.Settings.Retries = settingsSec.Key("retries").MustInt(3)
	cfg.Settings.AppendOutput = settingsSec.Key("append_output").MustBool(true)

	return cfg, nil
}

func splitTokenKey(s string) []string {
	return strings.Fields(strings.TrimSpace(s))
}

func chooseNetworks(networks map[string]Network) []string {
	names := make([]string, 0, len(networks))
	for name := range networks {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("\nДоступные сети:")
	for i, name := range names {
		fmt.Printf("%d. %s\n", i+1, name)
	}
	fmt.Println("0. Все сети")
	fmt.Print("Выберите сеть (введите номер): ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if choice == "0" || choice == "" {
		return names
	}

	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(names) {
		fmt.Println("Некорректный выбор, будет выбрана проверка всех сетей.")
		return names
	}

	return []string{names[idx-1]}
}

func loadKeys(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var keys []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		keys = append(keys, line)
	}
	return keys, scanner.Err()
}

func privateKeyToAddress(raw string) (common.Address, string, error) {
	normalized := strings.TrimSpace(raw)
	normalized = strings.TrimPrefix(normalized, "0x")
	if len(normalized) != 64 {
		return common.Address{}, "", fmt.Errorf("invalid private key length")
	}

	pk, err := crypto.HexToECDSA(normalized)
	if err != nil {
		return common.Address{}, "", err
	}

	publicKey := pk.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return common.Address{}, "", fmt.Errorf("cannot cast public key to ECDSA")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)
	return address, normalized, nil
}

func filterTokensByNetwork(tokens []Token, network string) []Token {
	result := make([]Token, 0)
	for _, token := range tokens {
		if token.Network == network {
			result = append(result, token)
		}
	}
	return result
}

func checkNativeBalance(client *ethclient.Client, out *os.File, settings Settings, privateKey string, address common.Address, network Network) {
	balanceWei, err := retryBigInt(settings, func(ctx context.Context) (*big.Int, error) {
		return client.BalanceAt(ctx, address, nil)
	})
	if err != nil {
		log.Printf("native balance error on %s for %s: %v", network.Name, address.Hex(), err)
		return
	}

	if balanceWei.Sign() <= 0 {
		return
	}

	balance := weiToDecimalString(balanceWei, 18)
	line := fmt.Sprintf("%s:%s:%s %s (%s)\n", privateKey, address.Hex(), balance, network.NativeToken, network.Name)
	if _, err := out.WriteString(line); err != nil {
		log.Printf("write error: %v", err)
		return
	}
	log.Printf("%s %s balance = %s", address.Hex(), network.NativeToken, balance)
}

func checkERC20Balances(client *ethclient.Client, out *os.File, settings Settings, privateKey string, address common.Address, network Network, tokens []Token) {
	parsedABI, err := abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		log.Printf("ERC20 ABI parse error: %v", err)
		return
	}

	decimalsCache := make(map[string]uint8)

	for _, token := range tokens {
		rawBalance, err := callERC20BalanceOf(client, parsedABI, token.Address, address, settings)
		if err != nil {
			log.Printf("ERC20 balanceOf error on %s for %s (%s): %v", network.Name, token.Symbol, token.Address.Hex(), err)
			continue
		}
		if rawBalance.Sign() <= 0 {
			continue
		}

		decimals, ok := decimalsCache[token.Address.Hex()]
		if !ok {
			decimals, err = callERC20Decimals(client, parsedABI, token.Address, settings)
			if err != nil {
				log.Printf("ERC20 decimals error on %s for %s (%s): %v", network.Name, token.Symbol, token.Address.Hex(), err)
				continue
			}
			decimalsCache[token.Address.Hex()] = decimals
		}

		balance := weiToDecimalString(rawBalance, int(decimals))
		line := fmt.Sprintf("%s:%s:%s %s (%s)\n", privateKey, address.Hex(), balance, token.Symbol, network.Name)
		if _, err := out.WriteString(line); err != nil {
			log.Printf("write error: %v", err)
			continue
		}
		log.Printf("%s %s balance = %s", address.Hex(), token.Symbol, balance)
	}
}

func callERC20BalanceOf(client *ethclient.Client, parsedABI abi.ABI, contract common.Address, owner common.Address, settings Settings) (*big.Int, error) {
	input, err := parsedABI.Pack("balanceOf", owner)
	if err != nil {
		return nil, err
	}

	result, err := retryBytes(settings, func(ctx context.Context) ([]byte, error) {
		msg := ethereum.CallMsg{To: &contract, Data: input}
		return client.CallContract(ctx, msg, nil)
	})
	if err != nil {
		return nil, err
	}

	outputs, err := parsedABI.Unpack("balanceOf", result)
	if err != nil {
		return nil, err
	}
	if len(outputs) != 1 {
		return nil, fmt.Errorf("unexpected balanceOf output count")
	}

	balance, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected balanceOf output type")
	}
	return balance, nil
}

func callERC20Decimals(client *ethclient.Client, parsedABI abi.ABI, contract common.Address, settings Settings) (uint8, error) {
	input, err := parsedABI.Pack("decimals")
	if err != nil {
		return 0, err
	}

	result, err := retryBytes(settings, func(ctx context.Context) ([]byte, error) {
		msg := ethereum.CallMsg{To: &contract, Data: input}
		return client.CallContract(ctx, msg, nil)
	})
	if err != nil {
		return 0, err
	}

	outputs, err := parsedABI.Unpack("decimals", result)
	if err != nil {
		return 0, err
	}
	if len(outputs) != 1 {
		return 0, fmt.Errorf("unexpected decimals output count")
	}

	switch v := outputs[0].(type) {
	case uint8:
		return v, nil
	case *big.Int:
		return uint8(v.Uint64()), nil
	default:
		return 0, fmt.Errorf("unexpected decimals output type")
	}
}

func retryBigInt(settings Settings, fn func(ctx context.Context) (*big.Int, error)) (*big.Int, error) {
	var lastErr error
	for i := 0; i < settings.Retries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(settings.RPCTimeoutSeconds)*time.Second)
		result, err := fn(ctx)
		cancel()
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func retryBytes(settings Settings, fn func(ctx context.Context) ([]byte, error)) ([]byte, error) {
	var lastErr error
	for i := 0; i < settings.Retries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(settings.RPCTimeoutSeconds)*time.Second)
		result, err := fn(ctx)
		cancel()
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func weiToDecimalString(value *big.Int, decimals int) string {
	if value == nil {
		return "0"
	}
	if decimals <= 0 {
		return value.String()
	}

	tenPow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	intPart := new(big.Int).Div(new(big.Int).Set(value), tenPow)
	fracPart := new(big.Int).Mod(new(big.Int).Set(value), tenPow)

	if fracPart.Sign() == 0 {
		return intPart.String()
	}

	fracStr := fracPart.String()
	if len(fracStr) < decimals {
		fracStr = strings.Repeat("0", decimals-len(fracStr)) + fracStr
	}
	fracStr = strings.TrimRight(fracStr, "0")
	if fracStr == "" {
		return intPart.String()
	}

	return intPart.String() + "." + fracStr
}

func _unused(_ *types.Transaction) {}