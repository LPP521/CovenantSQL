/*
 * Copyright 2018-2019 The CovenantSQL Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package internal

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh/terminal"

	pi "github.com/CovenantSQL/CovenantSQL/blockproducer/interfaces"
	"github.com/CovenantSQL/CovenantSQL/client"
	"github.com/CovenantSQL/CovenantSQL/conf"
	"github.com/CovenantSQL/CovenantSQL/crypto/asymmetric"
	"github.com/CovenantSQL/CovenantSQL/crypto/hash"
	"github.com/CovenantSQL/CovenantSQL/crypto/kms"
	"github.com/CovenantSQL/CovenantSQL/utils"
	"github.com/CovenantSQL/CovenantSQL/utils/log"
)

// These are general flags used by console and other commands.
var (
	configFile      string
	password        string
	withPassword    bool
	consoleLogLevel string // foreground console log level

	waitTxConfirmation bool // wait for transaction confirmation before exiting
	// Shard chain explorer/adapter stuff
	tmpPath    string // background observer and explorer block and log file path
	bgLogLevel string // background log level
	help       bool   // show sub command help message
)

func addCommonFlags(cmd *Command) {
	cmd.CommonFlag.BoolVar(&help, "help", false, "Show help message")
	cmd.CommonFlag.BoolVar(&withPassword, "with-password", false,
		"Enter the passphrase for private.key")

	// debugging flags.
	cmd.DebugFlag.StringVar(&password, "password", "",
		"Passphrase for encrypting private.key (NOT SAFE, for debug or script only)")
	cmd.DebugFlag.StringVar(&consoleLogLevel, "log-level", "info",
		"Console log level: trace debug info warning error fatal panic")
	cmd.DebugFlag.BoolVar(&asymmetric.BypassSignature, "bypass-signature", false,
		"Disable signature sign and verify, for testing")
}

func commonFlagsInit(cmd *Command) {
	if help {
		printCommandHelp(cmd)
		Exit()
	}

	if lvl, err := logrus.ParseLevel(consoleLogLevel); err != nil {
		ConsoleLog.SetLevel(log.InfoLevel)
	} else {
		ConsoleLog.SetLevel(lvl)
	}
}

func addConfigFlag(cmd *Command) {
	cmd.CommonFlag.StringVar(&configFile, "config", "~/.cql/config.yaml",
		"Config file for CovanantSQL (Usually no need to set, default is enough.)")
}

func configInit() {
	configFile = utils.HomeDirExpand(configFile)

	if password == "" {
		password = readMasterKey(!withPassword)
	}

	// init covenantsql driver
	if err := client.Init(configFile, []byte(password)); err != nil {
		ConsoleLog.WithError(err).Error("init covenantsql client failed")
		SetExitStatus(1)
		Exit()
	}

	ConsoleLog.WithField("path", configFile).Info("init config success")

	// TODO(leventeliu): discover more specific confirmation duration from config. We don't have
	// enough informations from config to do that currently, so just use a fixed and long enough
	// duration.
	waitTxConfirmationMaxDuration = 20 * conf.GConf.BPPeriod
}

func addWaitFlag(cmd *Command) {
	cmd.Flag.BoolVar(&waitTxConfirmation, "wait-tx-confirm", false, "Wait for transaction confirmation")
}

func wait(txHash hash.Hash) (err error) {
	var ctx, cancel = context.WithTimeout(context.Background(), waitTxConfirmationMaxDuration)
	defer cancel()
	var state pi.TransactionState
	state, err = client.WaitTxConfirmation(ctx, txHash)
	ConsoleLog.WithFields(logrus.Fields{
		"tx_hash":  txHash,
		"tx_state": state,
	}).WithError(err).Info("wait transaction confirmation")
	if err == nil && state != pi.TransactionStateConfirmed {
		err = errors.New("bad transaction state")
	}
	return
}

func addBgServerFlag(cmd *Command) {
	cmd.Flag.StringVar(&tmpPath, "tmp-path", "",
		"Background service temp file path, use \"dirname $(mktemp -u)\" to check it out")
	cmd.Flag.StringVar(&bgLogLevel, "bg-log-level", "info",
		"Background service log level: trace debug info warning error fatal panic")
}

func bgServerInit() {
	if tmpPath == "" {
		tmpPath = os.TempDir()
	}
	logPath := filepath.Join(tmpPath, "covenant_service.log")
	bgLog, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		ConsoleLog.Errorf("open log file failed: %s, %v", logPath, err)
		SetExitStatus(1)
		Exit()
	}

	log.SetOutput(bgLog)
	log.SetStringLevel(bgLogLevel, log.InfoLevel)
}

// readMasterKey reads the password of private key from terminal
func readMasterKey(skip bool) string {
	if skip {
		return ""
	}
	fmt.Println("Enter master key(press Enter for default: \"\"): ")
	bytePwd, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		ConsoleLog.Errorf("read master key failed: %v", err)
		SetExitStatus(1)
		Exit()
	}
	return string(bytePwd)
}

func getPublicFromConfig() *asymmetric.PublicKey {
	configFile = utils.HomeDirExpand(configFile)

	var err error
	// load config
	if conf.GConf, err = conf.LoadConfig(configFile); err != nil {
		ConsoleLog.WithError(err).Error("load config file failed")
		SetExitStatus(1)
		ExitIfErrors()
	}

	//if config has public, use it
	for _, node := range conf.GConf.KnownNodes {
		if node.ID == conf.GConf.ThisNodeID {
			if node.PublicKey != nil {
				ConsoleLog.Infof("use public key in config file: %s", configFile)
				return node.PublicKey
			}
			break
		}
	}

	//use config specific private key file(already init by configInit())
	ConsoleLog.Infof("generate public key directly from private key: %s", conf.GConf.PrivateKeyFile)
	privateKey, err := kms.LoadPrivateKey(conf.GConf.PrivateKeyFile, []byte(password))
	if err != nil {
		ConsoleLog.WithError(err).Error("load private key file failed")
		SetExitStatus(1)
		ExitIfErrors()
	}
	return privateKey.PubKey()
}

func storeDSN(dsnArray []string) {
	dsnFilePath := path.Join(conf.GConf.WorkingRoot, ".dsn")
	dsns := strings.Join(dsnArray, "\n")
	err := ioutil.WriteFile(dsnFilePath, []byte(dsns), 0644)
	if err != nil {
		ConsoleLog.WithError(err).Error("store dsn file failed")
		return
	}
}

func loadDSN() []string {
	dsnFilePath := path.Join(conf.GConf.WorkingRoot, ".dsn")
	dsns, err := ioutil.ReadFile(dsnFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			ConsoleLog.WithError(err).Error("load dsn file failed")
		}
		return nil
	}
	return strings.Split(string(dsns), "\n")
}

func storeOneDSN(dsn string) {
	dsnArray := loadDSN()
	dsnArray = append(dsnArray, dsn)
	storeDSN(dsnArray)
}
