package flashbot

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cryptoriums/telliot/pkg/private_file"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/log/level"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"golang.org/x/tools/godoc/util"
)

const (
	gasLimit    = 3_000_000
	gasPrice    = 10 * params.GWei
	blockNumMax = 10

	// Some ERC20 token with approve function.
	contractAddressGoerli  = "0xf74a5ca65e4552cff0f13b116113ccb493c580c5"
	contractAddressRinkeby = "0xdf032bc4b9dc2782bb09352007d4c57b75160b15"
	contractAddressMainnet = "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
)

var logger log.Logger

func init() {
	logger = log.With(
		log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)),
		"ts", log.TimestampFormat(func() time.Time { return time.Now().UTC() }, "jan 02 15:04:05.00"),
		"caller", log.Caller(5),
	)

	env, err := ioutil.ReadFile(".env")
	ExitOnError(logger, err)
	if !util.IsText(env) {
		level.Info(logger).Log("msg", "env file is encrypted")
		env = private_file.DecryptWithPasswordLoop(env)
	}

	rr := bytes.NewReader(env)
	envMap, err := godotenv.Parse(rr)
	ExitOnError(logger, err)

	// Copied from the godotenv source code.
	currentEnv := map[string]bool{}
	rawEnv := os.Environ()
	for _, rawEnvLine := range rawEnv {
		key := strings.Split(rawEnvLine, "=")[0]
		currentEnv[key] = true
	}

	for key, value := range envMap {
		if !currentEnv[key] {
			os.Setenv(key, value)
		}
	}
}

func Example() {
	ctx, cncl := context.WithTimeout(context.Background(), time.Hour)
	defer cncl()

	nodeURL := os.Getenv("NODE_URL")

	client, err := ethclient.DialContext(ctx, nodeURL)
	ExitOnError(logger, err)

	netID, err := client.NetworkID(ctx)
	ExitOnError(logger, err)
	level.Info(logger).Log("msg", "network", "id", netID.String(), "node", nodeURL)

	addr, err := GetContractAddress(netID)
	ExitOnError(logger, err)

	pubKey, privKey, err := GetKeys()
	ExitOnError(logger, err)

	flashbot, err := New(netID.Int64(), privKey)
	ExitOnError(logger, err)

	// Prepare the data for the TX.
	nonce, err := client.NonceAt(ctx, *pubKey, nil)
	ExitOnError(logger, err)

	abiP, err := abi.JSON(strings.NewReader(ContractABI))
	ExitOnError(logger, err)

	data, err := abiP.Pack(
		"approve",
		common.HexToAddress("0xd2ebc17f4dae9e512cae16da5ea9f55b7f65a623"),
		big.NewInt(1),
	)
	ExitOnError(logger, err)

	txHex, tx, err := flashbot.NewSignedTX(
		data,
		gasLimit,
		big.NewInt(gasPrice),
		big.NewInt(0),
		addr,
		nonce,
	)
	ExitOnError(logger, err)

	level.Info(logger).Log("msg", "created transaction", "hash", tx.Hash())

	blockNumber, err := client.BlockNumber(ctx)
	ExitOnError(logger, err)

	resp, err := flashbot.CallBundle(
		[]string{txHex},
	)
	ExitOnError(logger, err)

	level.Info(logger).Log("msg", "Called Bundle",
		"respStruct", fmt.Sprintf("%+v", resp),
	)

	for i := uint64(1); i < blockNumMax; i++ {
		resp, err = flashbot.SendBundle(
			[]string{txHex},
			blockNumber+i,
		)
		ExitOnError(logger, err)
	}

	level.Info(logger).Log("msg", "Sent Bundle",
		"blockMax", strconv.Itoa(int(blockNumMax)),
		"respStruct", fmt.Sprintf("%+v", resp),
	)

	// Output:
}

func ExitOnError(logger log.Logger, err error) {
	if err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}
}

func GetKeys() (*common.Address, *ecdsa.PrivateKey, error) {
	_privateKey := os.Getenv("ETH_PRIVATE_KEY")
	privateKey, err := crypto.HexToECDSA(strings.TrimSpace(_privateKey))
	if err != nil {
		return nil, nil, err
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, nil, errors.New("casting public key to ECDSA")
	}

	publicAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	return &publicAddress, privateKey, nil
}

func Keccak256(input []byte) [32]byte {
	hash := crypto.Keccak256(input)
	var hashed [32]byte
	copy(hashed[:], hash)

	return hashed
}

func GetContractAddress(networkID *big.Int) (common.Address, error) {
	switch netID := networkID.Int64(); netID {
	case 1:
		return common.HexToAddress(contractAddressMainnet), nil
	case 4:
		return common.HexToAddress(contractAddressRinkeby), nil
	case 5:
		return common.HexToAddress(contractAddressGoerli), nil
	default:
		return common.Address{}, errors.Errorf("network id not supported id:%v", netID)
	}
}

const ContractABI = `[
	{
	   "inputs":[
		  {
			 "internalType":"address",
			 "name":"spender",
			 "type":"address"
		  },
		  {
			 "internalType":"uint256",
			 "name":"value",
			 "type":"uint256"
		  }
	   ],
	   "name":"approve",
	   "outputs":[
		  {
			 "internalType":"bool",
			 "name":"",
			 "type":"bool"
		  }
	   ],
	   "stateMutability":"nonpayable",
	   "type":"function"
	}
 ]`
