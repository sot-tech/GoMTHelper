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
	"github.com/xlzd/gotp"
	mt "github.com/zelenin/go-tdlib/client"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	appVersion         = "0.275349w"
	chatsPageNum int32 = 100
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

type CFunc func(int64, string, string) error

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
	Thumbnail *MediaParams
}

type Telegram struct {
	Client           *mt.Client
	Messages         TGMessages
	Commands         map[string]CFunc
	BackendFunctions TGBackendFunction
	mtParameters     mt.TdlibParameters
	offset           int
	totp             *gotp.TOTP
	connected        bool
	fileUploadChan   chan string
	fileUploadMutex  sync.Mutex
	ownName          string
}

func (tg *Telegram) startF(chat int64, _, _ string) error {
	var err error
	var resp string
	resp = tg.Messages.Commands.Start
	tg.SendMsg(resp, []int64{chat}, false)
	return err
}

func (tg *Telegram) attachF(chat int64, _, _ string) error {
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

func (tg *Telegram) detachF(chat int64, _, _ string) error {
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

func (tg *Telegram) setAdminF(chat int64, _, args string) error {
	var err error
	if tg.BackendFunctions.AdminAdd == nil {
		err = errors.New("function AdminAdd not defined")
	} else {
		if tg.ValidateOTP(args) {
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

func (tg *Telegram) rmAdminF(chat int64, _, _ string) error {
	var err error
	if tg.BackendFunctions.AdminRm == nil || tg.BackendFunctions.AdminExist == nil {
		err = errors.New("function AdminRm|AdminExist not defined")
	} else {
		var exist bool
		if exist, err = tg.BackendFunctions.AdminExist(chat); err == nil {
			if exist {
				if err = tg.BackendFunctions.AdminRm(chat); err == nil {
					logger.Noticef("Admin deleted %d", chat)
					tg.SendMsg(tg.Messages.Commands.RmAdmin, []int64{chat}, false)
				}
			} else {
				logger.Infof("RmAdmin unauthorized %d", chat)
				tg.SendMsg(tg.Messages.Unauthorized, []int64{chat}, false)
			}
		}
	}
	return err
}

func (tg *Telegram) stateF(chat int64, _, _ string) error {
	var err error
	if tg.BackendFunctions.State == nil {
		err = errors.New("function State not defined")
	} else {
		var state string
		if state, err = tg.BackendFunctions.State(chat); err == nil {
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
	chat := msg.ChatId
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
		if strings.ContainsRune(cmdStr, '@') {
			cmdWords := strings.SplitN(cmdStr, "@", 2)
			if cmdWords[1] != tg.ownName{
				return
			}
			cmdStr = cmdWords[0]
		}
		cmd := tg.Commands[cmdStr]
		if cmd == nil && strings.ContainsRune(cmdStr, '_') {
			words = strings.SplitN(cmdStr, "_", 2)
			if len(words) > 0 {
				cmdStr = words[0]
			}
			if len(words) > 1 {
				args = strings.TrimSpace(args + " " + words[1])
			}
			cmd = tg.Commands[cmdStr]
		}
		if cmd == nil {
			logger.Warningf("Command not found: %s, chat: %d", cmdStr, chat)
			tg.SendMsg(tg.Messages.Commands.Unknown, []int64{chat}, false)
		} else if err := cmd(chat, cmdStr, args); err != nil {
			logger.Error(err)
			tg.SendMsg(tg.Messages.Error+err.Error(), []int64{chat}, false)
		}
	}

}

func (tg *Telegram) ValidateOTP(otp string) bool {
	var res bool
	if tg.totp != nil{
		res = tg.totp.Verify(otp, int(time.Now().Unix()))
	} else{
		logger.Warning("TOTP not initialised")
	}
	return res
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

func (tg *Telegram) RmCommand(cmd string) {
	delete(tg.Commands, cmd)
}

func (tg *Telegram) HandleUpdates() {
	if listener := tg.Client.GetListener(); listener != nil {
		defer listener.Close()
		for up := range listener.Updates {
			if !tg.connected {
				tg.fileUploadChan <- ""
				break
			}
			if up != nil && up.GetClass() == mt.ClassUpdate {
				switch up.GetType() {
				case mt.TypeUpdateNewMessage:
					upMsg := up.(*mt.UpdateNewMessage)
					if msg := upMsg.Message; msg != nil && !msg.IsOutgoing && msg.Content != nil &&
						msg.Content.MessageContentType() == mt.TypeMessageText {
						content := upMsg.Message.Content.(*mt.MessageText)
						if content.Text != nil && len(content.Text.Text) > 0 && content.Text.Text[0] == '/' {
							logger.Debug("Got new message:", upMsg.Message)
							go tg.processCommand(upMsg.Message)
						}
					}
				case mt.TypeUpdateFile:
					updateMsg := up.(*mt.UpdateFile)
					if updateMsg != nil && updateMsg.File != nil && updateMsg.File.Remote != nil {
						if updateMsg.File.Remote.IsUploadingCompleted {
							tg.fileUploadChan <- updateMsg.File.Remote.Id
							logger.Debug("File upload complete ", updateMsg.File.Remote.Id)
						} else {
							logger.Debugf("File uploading %d/%d bytes",
								updateMsg.File.Remote.UploadedSize,
								updateMsg.File.Size)
						}

					}
				}
			}
		}
	}
}

func (tg *Telegram) SendMsg(msgText string, chatIds []int64, formatted bool) {
	if msgText != "" && len(chatIds) > 0 {
		logger.Debugf("Sending message %s to %v", msgText, chatIds)
		var fMsg *mt.FormattedText
		if formatted {
			fMsg = FormatText(msgText)
		} else {
			fMsg = &mt.FormattedText{Text: msgText}
		}
		msg := mt.InputMessageText{
			Text:                  fMsg,
			DisableWebPagePreview: true,
			ClearDraft:            true,
		}
		for _, chatId := range chatIds {
			if chat, err := tg.GetChatTitle(chatId); err == nil {
				if len(chat) > 0 {
					req := &mt.SendMessageRequest{
						ChatId:              chatId,
						InputMessageContent: &msg,
					}
					if _, err := tg.Client.SendMessage(req); err == nil {
						logger.Debugf("Message to %s has been sent", chat)
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
	var sentFileId string
	for len(tg.fileUploadChan) > 0 {
		logger.Debug("Clean file upload channel, left ", len(tg.fileUploadChan))
		<-tg.fileUploadChan
	}
	for _, chatId := range chatIds {
		if chat, err := tg.GetChatTitle(chatId); err == nil {
			if len(chat) > 0 {
				req := &mt.SendMessageRequest{
					ChatId:              chatId,
					InputMessageContent: content,
				}
				if _, err := tg.Client.SendMessage(req); err == nil {
					if len(sentFileId) == 0 {
						logger.Debug("Waiting for file upload")
						sentFileId = <-tg.fileUploadChan
						logger.Info("Got image id ", sentFileId)
					}
					logger.Debugf("Media to %s has been sent", chat)
					if len(sentFileId) == 0 {
						logger.Warningf("Unable to get file Id")
						break
					}
					idSetter(&mt.InputFileRemote{Id: sentFileId})
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
			var thumbnail *mt.InputThumbnail
			if formatted {
				caption = FormatText(msgText)
			} else {
				caption = &mt.FormattedText{Text: msgText}
			}
			if photo.Thumbnail != nil && len(photo.Thumbnail.Path) > 0 {
				thumbnail = &mt.InputThumbnail{
					Thumbnail: &mt.InputFileLocal{Path: photo.Thumbnail.Path},
					Width:     photo.Thumbnail.Width,
					Height:    photo.Thumbnail.Height,
				}
			}
			msg := &mt.InputMessagePhoto{
				Photo:     &mt.InputFileLocal{Path: photo.Path},
				Thumbnail: thumbnail,
				Width:     photo.Width,
				Height:    photo.Height,
				Caption:   caption,
			}
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
			var thumbnail *mt.InputThumbnail
			if formatted {
				caption = FormatText(msgText)
			} else {
				caption = &mt.FormattedText{Text: msgText}
			}
			if video.Thumbnail != nil && len(video.Thumbnail.Path) > 0 {
				thumbnail = &mt.InputThumbnail{
					Thumbnail: &mt.InputFileLocal{Path: video.Thumbnail.Path},
					Width:     video.Thumbnail.Width,
					Height:    video.Thumbnail.Height,
				}
			}
			msg := &mt.InputMessageVideo{
				Video:             &mt.InputFileLocal{Path: video.Path},
				Thumbnail:         thumbnail,
				Width:             video.Width,
				Height:            video.Height,
				SupportsStreaming: video.Streaming,
				Caption:           caption,
			}
			videoSetter := func(id *mt.InputFileRemote) {
				msg.Video = id
			}
			tg.sendMediaMessage(chatIds, msg, videoSetter)
		}
	}
}

func (tg *Telegram) GetChatTitle(id int64) (string, error) {
	var err error
	var name string
	if chat, err := tg.Client.GetChat(&mt.GetChatRequest{ChatId: id}); err == nil {
		name = chat.Title
	}
	return name, err
}

func (tg *Telegram) GetChats() ([]int64, error) {
	var err error
	cl := &mt.ChatListMain{}
	allChats := make([]int64, 0, 50)
	var chatIdOffset int64
	offsetOrder := mt.JsonInt64(math.MaxInt64)
	for {
		var chats *mt.Chats
		req := &mt.GetChatsRequest{
			ChatList:     cl,
			OffsetOrder:  offsetOrder,
			OffsetChatId: chatIdOffset,
			Limit:        chatsPageNum,
		}
		if chats, err = tg.Client.GetChats(req); err == nil {
			if chats == nil || len(chats.ChatIds) == 0 {
				break
			}
			allChats = append(allChats, chats.ChatIds...)
			chatIdOffset = allChats[len(allChats)-1]
			var chat *mt.Chat
			if chat, err = tg.Client.GetChat(&mt.GetChatRequest{ChatId: chatIdOffset}); err == nil {
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

func (tg *Telegram) LoginAsBot(botToken string, logLevel int32) error {
	var err error
	auth := mt.BotAuthorizer(botToken)
	auth.TdlibParameters <- &tg.mtParameters
	logLevelRequest := mt.WithLogVerbosity(&mt.SetLogVerbosityLevelRequest{NewVerbosityLevel: logLevel})
	if tg.Client, err = mt.NewClient(auth, logLevelRequest); err == nil {
		var me *mt.User
		if me, err = tg.Client.GetMe(); err == nil {
			tg.connected = true
			tg.ownName = "@" + me.Username
			logger.Info("Authorized as", me.Username)
		}
	}
	return err
}

func (tg *Telegram) LoginAsUser(inputHandler func(string) (string, error), logLevel int32) error {
	var err, authErr error
	auth := mt.ClientAuthorizer()
	go func() {
		for {
			select {
			case state, ok := <-auth.State:
				if !ok {
					return
				}
				stateType := state.AuthorizationStateType()
				logger.Info(stateType)
				var inputChan chan string
				switch stateType {
				case mt.TypeAuthorizationStateWaitEncryptionKey:
					time.Sleep(time.Second)
					continue
				case mt.TypeAuthorizationStateClosed, mt.TypeAuthorizationStateClosing, mt.TypeAuthorizationStateLoggingOut:
					authErr = errors.New("connection closing " + stateType)
					return
				case mt.TypeAuthorizationStateWaitTdlibParameters:
					auth.TdlibParameters <- &tg.mtParameters
					continue
				case mt.TypeAuthorizationStateWaitPhoneNumber:
					inputChan = auth.PhoneNumber
				case mt.TypeAuthorizationStateWaitCode:
					inputChan = auth.Code
				case mt.TypeAuthorizationStateWaitPassword:
					inputChan = auth.Password
				case mt.TypeAuthorizationStateReady:
					return
				}
				if inputHandler != nil {
					if val, err := inputHandler(stateType); err == nil {
						inputChan <- val
					} else {
						authErr = err
						return
					}
				} else {
					authErr = errors.New("authorization handler not set")
					return
				}
			}
		}
	}()
	logLevelRequest := mt.WithLogVerbosity(&mt.SetLogVerbosityLevelRequest{NewVerbosityLevel: logLevel})
	if tg.Client, err = mt.NewClient(auth, logLevelRequest); err == nil {
		if authErr != nil {
			err = authErr
		} else {
			var me *mt.User
			if me, err = tg.Client.GetMe(); err == nil {
				tg.connected = true
				tg.ownName = "@" + me.Username
				logger.Info("Authorized as", me.Username)
				if _, err := tg.GetChats(); err != nil {
					logger.Warning(err)
				}
			}
		}
	}
	return err
}

func (tg *Telegram) Close() {
	if _, err := tg.Client.Close(); err != nil {
		logger.Error(err)
	}
	if _, err := tg.Client.Destroy(); err != nil {
		logger.Error(err)
	}
	tg.connected = false
	close(tg.fileUploadChan)
}

func New(apiId int32, apiHash, dbLocation, filesLocation, otpSeed string) *Telegram {
	params := mt.TdlibParameters{
		UseTestDc:              false,
		DatabaseDirectory:      dbLocation,
		FilesDirectory:         filesLocation,
		UseFileDatabase:        false,
		UseChatInfoDatabase:    false,
		UseMessageDatabase:     false,
		UseSecretChats:         false,
		ApiId:                  apiId,
		ApiHash:                apiHash,
		SystemLanguageCode:     "en",
		DeviceModel:            runtime.GOOS,
		SystemVersion:          "n/d",
		ApplicationVersion:     appVersion,
		EnableStorageOptimizer: true,
		IgnoreFileNames:        false,
	}
	var totp *gotp.TOTP
	if len(otpSeed) > 0{
		totp = gotp.NewDefaultTOTP(otpSeed)
	} else{
		logger.Warning("OTP seed not set, TOTP won't check passwords")
	}
	tg := &Telegram{
		Commands:       make(map[string]CFunc),
		mtParameters:   params,
		totp:           totp,
		fileUploadChan: make(chan string, 100),
	}
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
