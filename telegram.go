/*
 * BSD-3-Clause
 * Copyright 2020 sot (PR_713, C_rho_272)
 * Redistribution and use in source and binary forms, with or without modification,
 * are permitted provided that the following conditions are met:
 * 1. Redistributions of source code must retain the above copyright notice,
 * this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright notice,
 * this list of conditions and the following disclaimer in the documentation and/or
 * other materials provided with the distribution.
 * 3. Neither the name of the copyright holder nor the names of its contributors
 * may be used to endorse or promote products derived from this software without
 * specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 * WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
 * IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
 * INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING,
 * BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA,
 * OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY,
 * WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY
 * OF SUCH DAMAGE.
 */

package MTHelper

import (
	"errors"
	"github.com/op/go-logging"
	mt "github.com/Arman92/go-tdlib"
	"github.com/xlzd/gotp"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	appVersion          = "0.195284w"
	chatsPageNum  int32 = 100
	updatesBuffer       = 50
)

var logger = logging.MustGetLogger("sot-te.ch/TGHelper")

const (
	MtLogFatal = iota
	MtLogError
	MtLogWarning
	MtLogInfo
	MtLogDebug
	MtLogVerbose

	cmdStart    = "/start"
	cmdAttach   = "/attach"
	cmdDetach   = "/detach"
	cmdSetAdmin = "/setadmin"
	cmdRmAdmin  = "/rmadmin"
	cmdState    = "/state"
)

type CFunc func(int64, string) error

type TGBackendFunction struct {
	GetOffset  func() (int, error)
	SetOffset  func(int) error
	ChatExist  func(int64) (bool, error)
	ChatAdd    func(int64) error
	ChatRm     func(int64) error
	AdminExist func(int64) (bool, error)
	AdminAdd   func(int64) error
	AdminRm    func(int64) error
	State      func(int64) (string, error)
}

type TGCommandResponse struct {
	Start    string `json:"start"`
	Attach   string `json:"attach"`
	Detach   string `json:"detach"`
	SetAdmin string `json:"setadmin"`
	RmAdmin  string `json:"rmadmin"`
	Unknown  string `json:"unknown"`
}

type TGMessages struct {
	Commands     TGCommandResponse `json:"cmds"`
	Unauthorized string            `json:"auth"`
	Error        string            `json:"error"`
}

type MediaParams struct {
	Path      string
	Width     int32
	Height    int32
	Streaming bool
}

type Telegram struct {
	Client           *mt.Client
	Messages         TGMessages
	Commands         map[string]CFunc
	BackendFunctions TGBackendFunction
	offset           int
	totp             *gotp.TOTP
	IsBot            bool
	connected        bool
	fileUploadChan   chan string
	fileUploadMutex  sync.Mutex
}

func SetupMtLog(path string, level int) {
	mt.SetLogVerbosityLevel(level)
	mt.SetFilePath(path)
}

func (tg *Telegram) startF(chat int64, _ string) error {
	var err error
	var resp string
	resp = tg.Messages.Commands.Start
	tg.SendMsg(resp, []int64{chat}, false)
	return err
}

func (tg *Telegram) attachF(chat int64, _ string) error {
	var err error
	if tg.BackendFunctions.ChatAdd == nil {
		err = errors.New("function ChatAdd not defined")
	} else {
		if err = tg.BackendFunctions.ChatAdd(chat); err == nil {
			logger.Noticef("New chat added %d", chat)
			tg.SendMsg(tg.Messages.Commands.Attach, []int64{chat}, false)
		}
	}
	return err
}

func (tg *Telegram) detachF(chat int64, _ string) error {
	var err error
	if tg.BackendFunctions.ChatRm == nil {
		err = errors.New("function ChatRm not defined")
	} else {
		if err = tg.BackendFunctions.ChatRm(chat); err == nil {
			logger.Noticef("Chat deleted %d", chat)
			tg.SendMsg(tg.Messages.Commands.Detach, []int64{chat}, false)
		}
	}
	return err
}

func (tg *Telegram) setAdminF(chat int64, args string) error {
	var err error
	if tg.BackendFunctions.AdminAdd == nil {
		err = errors.New("function AdminAdd not defined")
	} else {
		if tg.totp.Verify(args, int(time.Now().Unix())) {
			if err = tg.BackendFunctions.AdminAdd(chat); err == nil {
				logger.Noticef("New admin added %d", chat)
				tg.SendMsg(tg.Messages.Commands.SetAdmin, []int64{chat}, false)
			}
		} else {
			logger.Infof("SetAdmin unauthorized %d", chat)
			tg.SendMsg(tg.Messages.Unauthorized, []int64{chat}, false)
		}
	}
	return err
}

func (tg *Telegram) rmAdminF(chat int64, _ string) error {
	var err error
	if tg.BackendFunctions.AdminRm == nil || tg.BackendFunctions.AdminExist == nil {
		err = errors.New("function AdminRm|AdminExist not defined")
	} else {
		var exist bool
		if exist, err = tg.BackendFunctions.AdminExist(chat); err == nil {
			if exist {
				if err = tg.BackendFunctions.AdminAdd(chat); err == nil {
					logger.Noticef("Admin deleted %d", chat)
					tg.SendMsg(tg.Messages.Commands.SetAdmin, []int64{chat}, false)
				}
			} else {
				logger.Infof("RmAdmin unauthorized %d", chat)
				tg.SendMsg(tg.Messages.Unauthorized, []int64{chat}, false)
			}
		}
	}
	return err
}

func (tg *Telegram) stateF(chat int64, _ string) error {
	var err error
	if tg.BackendFunctions.State == nil {
		err = errors.New("function State not defined")
	} else {
		var state string
		if state, err = tg.BackendFunctions.State(chat); err != nil {
			tg.SendMsg(state, []int64{chat}, false)
		}
	}
	return err
}

func (tg *Telegram) getOffset() (int, error) {
	return tg.offset, nil
}

func (tg *Telegram) setOffset(offset int) error {
	tg.offset = offset
	return nil
}

func (tg *Telegram) processCommand(msg *mt.Message) {
	chat := msg.ChatID
	content := msg.Content.(*mt.MessageText)
	if content != nil && content.Text != nil {
		words := strings.SplitN(content.Text.Text, " ", 2)
		var cmdStr, args string
		if len(words) > 0 {
			cmdStr = words[0]
		}
		if len(words) > 1 {
			args = words[1]
		}
		cmd := tg.Commands[cmdStr]
		if cmd == nil {
			logger.Warningf("Command not found: %s, chat: %d", cmdStr, chat)
			tg.SendMsg(tg.Messages.Commands.Unknown, []int64{chat}, false)
		} else if err := cmd(chat, args); err != nil {
			logger.Error(err)
			tg.SendMsg(tg.Messages.Error+err.Error(), []int64{chat}, false)
		}
	}

}

func (tg *Telegram) AddCommand(cmd string, cmdFunc CFunc) error {
	var err error
	if cmd == "" || cmdFunc == nil {
		err = errors.New("unable to add empty command")
	} else if tg.Commands == nil {
		tg.Commands = make(map[string]CFunc)
	}
	tg.Commands[cmd] = cmdFunc
	return err
}

func (tg *Telegram) HandleUpdates() {
	msgReceiver := tg.Client.AddEventReceiver(&mt.UpdateNewMessage{}, func(msg *mt.TdMessage) bool {
		updateMsg := (*msg).(*mt.UpdateNewMessage)
		if updateMsg != nil && updateMsg.Message != nil && !updateMsg.Message.IsOutgoing &&
			updateMsg.Message.Content != nil && updateMsg.Message.Content.GetMessageContentEnum() == mt.MessageTextType {
			content := updateMsg.Message.Content.(*mt.MessageText)
			return content.Text != nil && len(content.Text.Text) > 0 && content.Text.Text[0] == '/'
		}
		return false
	}, updatesBuffer)
	fileUploadReceiver := tg.Client.AddEventReceiver(&mt.UpdateFile{}, func(msg *mt.TdMessage) bool {
		return true
	}, updatesBuffer)
	go func() {
		if msgReceiver.Chan != nil {
			for up := range msgReceiver.Chan {
				if !tg.connected {
					break
				}
				updateMsg := up.(*mt.UpdateNewMessage)
				logger.Debugf("Got new update: %v", updateMsg.Message)
				go tg.processCommand(updateMsg.Message)
			}
		} else {
			logger.Error("Unable to get telegram update channel is nil")
		}
	}()
	go func() {
		if fileUploadReceiver.Chan != nil {
			for up := range fileUploadReceiver.Chan {
				if !tg.connected {
					tg.fileUploadChan <- ""
					break
				}
				updateMsg := up.(*mt.UpdateFile)
				if updateMsg != nil && updateMsg.File != nil && updateMsg.File.Remote != nil {
					if updateMsg.File.Remote.IsUploadingCompleted {
						logger.Debug("File upload complete ", updateMsg.File.Remote.ID)
						tg.fileUploadChan <- updateMsg.File.Remote.ID
					} else{
						logger.Debugf("File uploading %d/%d bytes", updateMsg.File.Remote.UploadedSize, updateMsg.File.Size)
					}

				}
			}
		} else {
			logger.Error("Unable to get telegram update channel is nil")
		}
	}()
}

func (tg *Telegram) SendMsg(msgText string, chatIds []int64, formatted bool) {
	if msgText != "" && len(chatIds) > 0 {
		logger.Debugf("Sending message %s to %v", msgText, chatIds)
		var fMsg *mt.FormattedText
		if formatted {
			fMsg = FormatText(msgText)
		} else {
			fMsg = mt.NewFormattedText(msgText, nil)
		}
		msg := mt.NewInputMessageText(fMsg, true, true)
		for _, chatId := range chatIds {
			if chat, err := tg.Client.GetChat(chatId); err == nil {
				if chat != nil {
					if _, err := tg.Client.SendMessage(chatId, 0, false,
						false, nil, msg); err == nil {
						logger.Debugf("Message to %d has been sent", chatId)
					} else {
						logger.Error(err)
					}
				} else {
					logger.Errorf("Chat %d not found", chatId)
				}
			} else {
				logger.Error(err)
			}
		}
	}
}

func (tg *Telegram) sendMediaMessage(chatIds []int64, content mt.InputMessageContent, idSetter func(*mt.InputFileRemote)) {
	tg.fileUploadMutex.Lock()
	defer tg.fileUploadMutex.Unlock()
	uploadCnt := 0
	var sentFileId string
	for _, chatId := range chatIds {
		if chat, err := tg.Client.GetChat(chatId); err == nil {
			if chat != nil {
				if _, err := tg.Client.SendMessage(chatId, 0, false,
					false, nil, content); err == nil {
					// First upload as file, second as remote file id, next - as update message, so there's no updateFile event
					if uploadCnt < 2 {
						sentFileId = <-tg.fileUploadChan
					}
					if uploadCnt == 0 {
						if len(sentFileId) == 0 {
							logger.Warningf("Unable to get file Id")
							break
						}
						idSetter(mt.NewInputFileRemote(sentFileId))
					}
					uploadCnt++
				} else {
					logger.Error(err)
				}
			} else {
				logger.Errorf("Chat %d not found", chatId)
			}
		}
	}
}

func (tg *Telegram) SendPhoto(photo MediaParams, msgText string, chatIds []int64, formatted bool) {
	if len(chatIds) > 0 {
		if len(photo.Path) == 0 {
			logger.Warning("Photo is empty, sending as text")
			tg.SendMsg(msgText, chatIds, formatted)
		} else {
			logger.Debugf("Sending photo message %s to %v", msgText, chatIds)
			var caption *mt.FormattedText
			if formatted {
				caption = FormatText(msgText)
			} else {
				caption = mt.NewFormattedText(msgText, nil)
			}
			msg := mt.NewInputMessagePhoto(mt.NewInputFileLocal(photo.Path), nil, nil, photo.Width,
				photo.Height, caption, 0)
			photoSetter := func(id *mt.InputFileRemote) {
				msg.Photo = id
			}
			tg.sendMediaMessage(chatIds, msg, photoSetter)
		}
	}
}

func (tg *Telegram) SendVideo(video MediaParams, msgText string, chatIds []int64, formatted bool) {
	if len(chatIds) > 0 {
		if len(video.Path) == 0 {
			logger.Warning("Video is empty, sending as text")
			tg.SendMsg(msgText, chatIds, formatted)
		} else {
			logger.Debugf("Sending video message %s to %v", msgText, chatIds)
			var caption *mt.FormattedText
			if formatted {
				caption = FormatText(msgText)
			} else {
				caption = mt.NewFormattedText(msgText, nil)
			}
			msg := mt.NewInputMessageVideo(mt.NewInputFileLocal(video.Path), nil, nil, 0,
				video.Width, video.Height, video.Streaming, caption, 0)
			videoSetter := func(id *mt.InputFileRemote) {
				msg.Video = id
			}
			tg.sendMediaMessage(chatIds, msg, videoSetter)
		}
	}
}

func (tg *Telegram) GetChats() ([]int64, error) {
	var err error
	allChats := make([]int64, 0, 50)
	var chatIdOffset int64
	offsetOrder := mt.JSONInt64(math.MaxInt64)
	for {
		var chats *mt.Chats
		if chats, err = tg.Client.GetChats(offsetOrder, chatIdOffset, chatsPageNum); err == nil {
			if chats == nil || len(chats.ChatIDs) == 0 {
				break
			}
			allChats = append(allChats, chats.ChatIDs...)
			chatIdOffset = allChats[len(allChats)-1]
			var chat *mt.Chat
			if chat, err = tg.Client.GetChat(chatIdOffset); err == nil {
				if chat != nil {
					offsetOrder = chat.Order
				}
			} else {
				allChats = nil
				break
			}
		} else {
			allChats = nil
			break
		}
	}
	return allChats, err
}

func (tg *Telegram) Connect(timeout int) error {
	return tg.Login(nil, timeout)
}

func (tg *Telegram) Login(inputHandler func(string) string, timeout int) error {
	var err error
connect:
	for try := 0; try < timeout || timeout < 0; try++ {
		var authState mt.AuthorizationState
		if authState, err = tg.Client.Authorize(); err == nil {
			stateEnum := authState.GetAuthorizationStateEnum()
			switch stateEnum {
			case mt.AuthorizationStateWaitEncryptionKeyType:
				logger.Info(string(stateEnum))
				time.Sleep(10 * time.Second)
				continue
			case mt.AuthorizationStateReadyType:
				var me *mt.User
				if me, err = tg.Client.GetMe(); err == nil {
					tg.connected = true
					tg.IsBot = me.Type.GetUserTypeEnum() == mt.UserTypeBotType
					logger.Infof("Authorized as %s", me.Username)
					if !tg.IsBot {
						if chats, err := tg.GetChats(); err == nil {
							logger.Debugf("Reloaded chats: %v", chats)
						}
					}
				}
				break connect
			case mt.AuthorizationStateClosedType, mt.AuthorizationStateClosingType, mt.AuthorizationStateLoggingOutType:
				err = errors.New("connection closing " + string(stateEnum))
				break connect
			case mt.AuthorizationStateWaitTdlibParametersType:
				err = errors.New("required parameters not set " + string(stateEnum))
				break connect
			case mt.AuthorizationStateWaitPhoneNumberType:
				if inputHandler == nil {
					err = errors.New("unauthorized")
					break connect
				} else {
					number := inputHandler(string(stateEnum))
					if strings.IndexRune(number, ':') > 0 {
						_, err = tg.Client.CheckAuthenticationBotToken(number)
					} else {
						_, err = tg.Client.SendPhoneNumber(number)
					}
					if err != nil {
						break connect
					}
				}
			case mt.AuthorizationStateWaitCodeType:
				if inputHandler == nil {
					err = errors.New("unauthorized")
					break connect
				} else if _, err = tg.Client.SendAuthCode(inputHandler(string(stateEnum))); err != nil {
					break connect
				}
			case mt.AuthorizationStateWaitPasswordType:
				if inputHandler == nil {
					err = errors.New("unauthorized")
					break connect
				} else if _, err = tg.Client.SendAuthPassword(inputHandler(string(stateEnum))); err != nil {
					break connect
				}
			}
		} else {
			break
		}
	}
	return err
}

func (tg *Telegram) Close() {
	tg.Close()
	tg.Client.DestroyInstance()
	tg.connected = false
}

func New(apiId, apiHash, dbLocation, filesLocation, otpSeed string) *Telegram {
	tg := &Telegram{
		Commands:       make(map[string]CFunc),
		totp:           gotp.NewDefaultTOTP(otpSeed),
		fileUploadChan: make(chan string),
	}
	tg.Client = mt.NewClient(mt.Config{
		APIID:                  apiId,
		APIHash:                apiHash,
		SystemLanguageCode:     "en",
		DeviceModel:            runtime.GOOS,
		SystemVersion:          "n/d",
		ApplicationVersion:     appVersion,
		UseTestDataCenter:      false,
		DatabaseDirectory:      dbLocation,
		FileDirectory:          filesLocation,
		UseFileDatabase:        false,
		UseChatInfoDatabase:    false,
		UseMessageDatabase:     false,
		UseSecretChats:         false,
		EnableStorageOptimizer: true,
		IgnoreFileNames:        false,
	})
	tg.BackendFunctions = TGBackendFunction{
		GetOffset: tg.getOffset,
		SetOffset: tg.setOffset,
	}
	_ = tg.AddCommand(cmdStart, tg.startF)
	_ = tg.AddCommand(cmdAttach, tg.attachF)
	_ = tg.AddCommand(cmdDetach, tg.detachF)
	_ = tg.AddCommand(cmdSetAdmin, tg.setAdminF)
	_ = tg.AddCommand(cmdRmAdmin, tg.rmAdminF)
	_ = tg.AddCommand(cmdState, tg.stateF)
	return tg
}
