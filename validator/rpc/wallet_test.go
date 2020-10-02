package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	ptypes "github.com/gogo/protobuf/types"
	"github.com/google/uuid"
	pb "github.com/prysmaticlabs/prysm/proto/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/event"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	v2 "github.com/prysmaticlabs/prysm/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/wallet"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
)

func createDirectWalletWithAccounts(t testing.TB, numAccounts int) (*Server, [][]byte) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	w, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	require.NoError(t, w.SaveHashedPassword(ctx))
	km, err := w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	ss := &Server{
		keymanager: km,
		wallet:     w,
	}
	// First we import accounts into the wallet.
	encryptor := keystorev4.New()
	keystores := make([]string, numAccounts)
	pubKeys := make([][]byte, len(keystores))
	for i := 0; i < len(keystores); i++ {
		privKey := bls.RandKey()
		pubKey := fmt.Sprintf("%x", privKey.PublicKey().Marshal())
		id, err := uuid.NewRandom()
		require.NoError(t, err)
		cryptoFields, err := encryptor.Encrypt(privKey.Marshal(), strongPass)
		require.NoError(t, err)
		item := &v2keymanager.Keystore{
			Crypto:  cryptoFields,
			ID:      id.String(),
			Version: encryptor.Version(),
			Pubkey:  pubKey,
			Name:    encryptor.Name(),
		}
		encodedFile, err := json.MarshalIndent(item, "", "\t")
		require.NoError(t, err)
		keystores[i] = string(encodedFile)
		pubKeys[i] = privKey.PublicKey().Marshal()
	}
	_, err = ss.ImportKeystores(ctx, &pb.ImportKeystoresRequest{
		KeystoresImported: keystores,
		KeystoresPassword: strongPass,
	})
	require.NoError(t, err)
	ss.keymanager, err = ss.wallet.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	return ss, pubKeys
}

func TestServer_CreateWallet_Direct(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	s := &Server{
		walletInitializedFeed: new(event.Feed),
	}
	req := &pb.CreateWalletRequest{
		WalletPath:        localWalletDir,
		Keymanager:        pb.KeymanagerKind_DIRECT,
		WalletPassword:    strongPass,
		KeystoresPassword: strongPass,
	}
	// We delete the directory at defaultWalletPath as CreateWallet will return an error if it tries to create a wallet
	// where a directory already exists
	require.NoError(t, os.RemoveAll(defaultWalletPath))
	_, err := s.CreateWallet(ctx, req)
	require.ErrorContains(t, "No keystores included for import", err)

	req.KeystoresImported = []string{"badjson"}
	_, err = s.CreateWallet(ctx, req)
	require.ErrorContains(t, "Not a valid EIP-2335 keystore", err)

	encryptor := keystorev4.New()
	keystores := make([]string, 3)
	for i := 0; i < len(keystores); i++ {
		privKey := bls.RandKey()
		pubKey := fmt.Sprintf("%x", privKey.PublicKey().Marshal())
		id, err := uuid.NewRandom()
		require.NoError(t, err)
		cryptoFields, err := encryptor.Encrypt(privKey.Marshal(), strongPass)
		require.NoError(t, err)
		item := &v2keymanager.Keystore{
			Crypto:  cryptoFields,
			ID:      id.String(),
			Version: encryptor.Version(),
			Pubkey:  pubKey,
			Name:    encryptor.Name(),
		}
		encodedFile, err := json.MarshalIndent(item, "", "\t")
		require.NoError(t, err)
		keystores[i] = string(encodedFile)
	}
	req.KeystoresImported = keystores
	_, err = s.CreateWallet(ctx, req)
	require.NoError(t, err)
}

func TestServer_CreateWallet_Derived(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	s := &Server{
		walletInitializedFeed: new(event.Feed),
	}
	req := &pb.CreateWalletRequest{
		WalletPath:     localWalletDir,
		Keymanager:     pb.KeymanagerKind_DERIVED,
		WalletPassword: strongPass,
		NumAccounts:    0,
	}
	// We delete the directory at defaultWalletPath as CreateWallet will return an error if it tries to create a wallet
	// where a directory already exists
	require.NoError(t, os.RemoveAll(defaultWalletPath))
	_, err := s.CreateWallet(ctx, req)
	require.ErrorContains(t, "Must create at least 1 validator account", err)

	req.NumAccounts = 2
	_, err = s.CreateWallet(ctx, req)
	require.ErrorContains(t, "Must include mnemonic", err)

	mnemonicResp, err := s.GenerateMnemonic(ctx, &ptypes.Empty{})
	require.NoError(t, err)
	req.Mnemonic = mnemonicResp.Mnemonic

	_, err = s.CreateWallet(ctx, req)
	require.NoError(t, err)

	// Now trying to create a wallet where a previous wallet already exists.  We expect an error.
	_, err = s.CreateWallet(ctx, req)
	require.ErrorContains(t, "wallet already exists at this location", err)
}

func TestServer_WalletConfig_NoWalletFound(t *testing.T) {
	s := &Server{}
	resp, err := s.WalletConfig(context.Background(), &ptypes.Empty{})
	require.NoError(t, err)
	assert.DeepEqual(t, resp, &pb.WalletResponse{})
}

func TestServer_WalletConfig(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	s := &Server{
		walletInitializedFeed: new(event.Feed),
	}
	// We attempt to create the wallet.
	w, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	require.NoError(t, w.SaveHashedPassword(ctx))
	km, err := w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	s.wallet = w
	s.keymanager = km
	resp, err := s.WalletConfig(ctx, &ptypes.Empty{})
	require.NoError(t, err)

	expectedConfig := direct.DefaultKeymanagerOpts()
	enc, err := json.Marshal(expectedConfig)
	require.NoError(t, err)
	var jsonMap map[string]string
	require.NoError(t, json.Unmarshal(enc, &jsonMap))
	assert.DeepEqual(t, resp, &pb.WalletResponse{
		WalletPath:       localWalletDir,
		KeymanagerKind:   pb.KeymanagerKind_DIRECT,
		KeymanagerConfig: jsonMap,
	})
}

func TestServer_ChangePassword_Preconditions(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	ss := &Server{}
	_, err := ss.ChangePassword(ctx, &pb.ChangePasswordRequest{
		Password: "",
	})
	assert.ErrorContains(t, noWalletMsg, err)
	// We attempt to create the wallet.
	w, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Derived,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	km, err := w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	ss.wallet = w
	ss.walletInitialized = true
	ss.keymanager = km
	_, err = ss.ChangePassword(ctx, &pb.ChangePasswordRequest{
		Password: "",
	})
	assert.ErrorContains(t, "cannot be empty", err)
	_, err = ss.ChangePassword(ctx, &pb.ChangePasswordRequest{
		Password:             "abc",
		PasswordConfirmation: "def",
	})
	assert.ErrorContains(t, "does not match", err)
}

func TestServer_ChangePassword_DirectKeymanager(t *testing.T) {
	ss, _ := createDirectWalletWithAccounts(t, 1)
	newPassword := "NewPassw0rdz%%%%pass"
	_, err := ss.ChangePassword(context.Background(), &pb.ChangePasswordRequest{
		Password:             newPassword,
		PasswordConfirmation: newPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, ss.wallet.Password(), newPassword)
}

func TestServer_ChangePassword_DerivedKeymanager(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	// We attempt to create the wallet.
	w, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Derived,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	km, err := w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	ss := &Server{}
	ss.wallet = w
	ss.walletInitialized = true
	ss.keymanager = km
	newPassword := "NewPassw0rdz%%%%pass"
	_, err = ss.ChangePassword(ctx, &pb.ChangePasswordRequest{
		Password:             newPassword,
		PasswordConfirmation: newPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, w.Password(), newPassword)
}

func TestServer_HasWallet(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	ss := &Server{}
	// First delete the created folder and check the response
	require.NoError(t, os.RemoveAll(defaultWalletPath))
	resp, err := ss.HasWallet(ctx, &ptypes.Empty{})
	require.NoError(t, err)
	assert.DeepEqual(t, &pb.HasWalletResponse{
		WalletExists: false,
	}, resp)

	// We now create the folder but without a valid wallet, i.e. lacking a subdirectory such as 'direct'
	// We expect an empty directory to behave similarly as if there were no directory
	require.NoError(t, os.MkdirAll(defaultWalletPath, os.ModePerm))
	resp, err = ss.HasWallet(ctx, &ptypes.Empty{})
	require.NoError(t, err)
	assert.DeepEqual(t, &pb.HasWalletResponse{
		WalletExists: false,
	}, resp)

	// We attempt to create the wallet.
	_, err = v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	resp, err = ss.HasWallet(ctx, &ptypes.Empty{})
	require.NoError(t, err)
	assert.DeepEqual(t, &pb.HasWalletResponse{
		WalletExists: true,
	}, resp)
}

func TestServer_ImportKeystores_FailedPreconditions_WrongKeymanagerKind(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	w, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Derived,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	km, err := w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	ss := &Server{
		wallet:     w,
		keymanager: km,
	}
	_, err = ss.ImportKeystores(ctx, &pb.ImportKeystoresRequest{})
	assert.ErrorContains(t, "Only Non-HD wallets can import", err)
}

func TestServer_ImportKeystores_FailedPreconditions(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	w, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	require.NoError(t, w.SaveHashedPassword(ctx))
	km, err := w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	ss := &Server{
		keymanager: km,
	}
	_, err = ss.ImportKeystores(ctx, &pb.ImportKeystoresRequest{})
	assert.ErrorContains(t, "No wallet initialized", err)
	ss.wallet = w
	_, err = ss.ImportKeystores(ctx, &pb.ImportKeystoresRequest{})
	assert.ErrorContains(t, "Password required for keystores", err)
	_, err = ss.ImportKeystores(ctx, &pb.ImportKeystoresRequest{
		KeystoresPassword: strongPass,
	})
	assert.ErrorContains(t, "No keystores included for import", err)
	_, err = ss.ImportKeystores(ctx, &pb.ImportKeystoresRequest{
		KeystoresPassword: strongPass,
		KeystoresImported: []string{"badjson"},
	})
	assert.ErrorContains(t, "Not a valid EIP-2335 keystore", err)
}

func TestServer_ImportKeystores_OK(t *testing.T) {
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	ctx := context.Background()
	strongPass := "29384283xasjasd32%%&*@*#*"
	w, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	require.NoError(t, w.SaveHashedPassword(ctx))
	km, err := w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	ss := &Server{
		keymanager: km,
		wallet:     w,
	}

	// Create 3 keystores.
	encryptor := keystorev4.New()
	keystores := make([]string, 3)
	pubKeys := make([][]byte, 3)
	for i := 0; i < len(keystores); i++ {
		privKey := bls.RandKey()
		pubKey := fmt.Sprintf("%x", privKey.PublicKey().Marshal())
		id, err := uuid.NewRandom()
		require.NoError(t, err)
		cryptoFields, err := encryptor.Encrypt(privKey.Marshal(), strongPass)
		require.NoError(t, err)
		item := &v2keymanager.Keystore{
			Crypto:  cryptoFields,
			ID:      id.String(),
			Version: encryptor.Version(),
			Pubkey:  pubKey,
			Name:    encryptor.Name(),
		}
		encodedFile, err := json.MarshalIndent(item, "", "\t")
		require.NoError(t, err)
		keystores[i] = string(encodedFile)
		pubKeys[i] = privKey.PublicKey().Marshal()
	}

	// Check the wallet has no accounts to start with.
	keys, err := km.FetchValidatingPublicKeys(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, len(keys))

	// Import the 3 keystores and verify the wallet has 3 new accounts.
	res, err := ss.ImportKeystores(ctx, &pb.ImportKeystoresRequest{
		KeystoresPassword: strongPass,
		KeystoresImported: keystores,
	})
	require.NoError(t, err)
	assert.DeepEqual(t, &pb.ImportKeystoresResponse{
		ImportedPublicKeys: pubKeys,
	}, res)

	km, err = w.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	keys, err = km.FetchValidatingPublicKeys(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, len(keys))
}
