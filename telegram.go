/*
 * BSD-3-Clause
 * Copyright 2021 sot (PR_713, C_rho_272)
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
	"math"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
	"github.com/xlzd/gotp"
	mt "github.com/zelenin/go-tdlib/client"
)

var (
	defaultLogger = logging.MustGetLogger("sot-te.ch/MTHelper")
	AppVersion    = "0.275349w"
)

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

	cmdMaxLen = 1000

	chatsPageNum int32 = math.MaxInt32
)

type TgLogger interface {
	Error(args ...interface{})
	Warning(args ...interface{})
	Notice(args ...interface{})
	Info(args ...interface{})
	Debug(args ...interface{})
}

type nopLogger struct{}

func (nopLogger) Error(...interface{}) {
}

func (nopLogger) Warning(...interface{}) {
}

func (nopLogger) Notice(...interface{}) {
}

func (nopLogger) Info(...interface{}) {
}

func (nopLogger) Debug(...interface{}) {
}

var silentLogger TgLogger = nopLogger{}

type CommandFunc func(chatId int64, cmd string, args []string) error
type FileUploadCallback func(path, id string)

type TGBackendFunction struct {
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
	Client                  *mt.Client
	Messages                TGMessages
	Commands                map[string]CommandFunc
	BackendFunctions        TGBackendFunction
	mtParameters            *mt.SetTdlibParametersRequest
	totp                    *gotp.TOTP
	fileCallbacks           sync.Map
	ownNames                []string
	logger                  TgLogger
	onceStarter, onceCloser sync.Once
	stopWaiter              sync.WaitGroup
}

func (tg *Telegram) startF(chat int64, _ string, _ []string) error {
	resp := tg.Messages.Commands.Start
	tg.SendMsg(resp, []int64{chat}, false)
	return nil
}

func (tg *Telegram) attachF(chat int64, _ string, _ []string) error {
	var err error
	if tg.BackendFunctions.ChatAdd == nil {
		err = errors.New("function ChatAdd not defined")
	} else {
		if err = tg.BackendFunctions.ChatAdd(chat); err == nil {
			tg.logger.Notice("New chat added ", chat)
			tg.SendMsg(tg.Messages.Commands.Attach, []int64{chat}, false)
		}
	}
	return err
}

func (tg *Telegram) detachF(chat int64, _ string, _ []string) error {
	var err error
	if tg.BackendFunctions.ChatRm == nil {
		err = errors.New("function ChatRm not defined")
	} else {
		if err = tg.BackendFunctions.ChatRm(chat); err == nil {
			tg.logger.Notice("Chat deleted ", chat)
			tg.SendMsg(tg.Messages.Commands.Detach, []int64{chat}, false)
		}
	}
	return err
}

func (tg *Telegram) setAdminF(chat int64, _ string, args []string) error {
	var err error
	if tg.BackendFunctions.AdminAdd == nil {
		err = errors.New("function AdminAdd not defined")
	} else {
		if tg.ValidateOTP(args[0]) {
			if err = tg.BackendFunctions.AdminAdd(chat); err == nil {
				tg.logger.Notice("New admin added ", chat)
				tg.SendMsg(tg.Messages.Commands.SetAdmin, []int64{chat}, false)
			}
		} else {
			tg.logger.Info("SetAdmin unauthorized ", chat)
			tg.SendMsg(tg.Messages.Unauthorized, []int64{chat}, false)
		}
	}
	return err
}

func (tg *Telegram) rmAdminF(chat int64, _ string, _ []string) error {
	var err error
	if tg.BackendFunctions.AdminRm == nil || tg.BackendFunctions.AdminExist == nil {
		err = errors.New("function AdminRm|AdminExist not defined")
	} else {
		var exist bool
		if exist, err = tg.BackendFunctions.AdminExist(chat); err == nil {
			if exist {
				if err = tg.BackendFunctions.AdminRm(chat); err == nil {
					tg.logger.Notice("Admin deleted ", chat)
					tg.SendMsg(tg.Messages.Commands.RmAdmin, []int64{chat}, false)
				}
			} else {
				tg.logger.Info("RmAdmin unauthorized ", chat)
				tg.SendMsg(tg.Messages.Unauthorized, []int64{chat}, false)
			}
		}
	}
	return err
}

func (tg *Telegram) stateF(chat int64, _ string, _ []string) error {
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

func (tg *Telegram) processCommand(msg *mt.Message) {
	chat := msg.ChatId
	content := msg.Content.(*mt.MessageText)
	if content != nil && content.Text != nil && len(content.Text.Text) <= cmdMaxLen {
		words := strings.Split(content.Text.Text, " ")
		var cmdStr string
		args := make([]string, 0)
		if len(words) > 0 {
			cmdStr = words[0]
		}
		if len(words) > 1 {
			args = append(args, words[1:]...)
		}
		if strings.ContainsRune(cmdStr, '@') {
			cmdWords := strings.SplitN(cmdStr, "@", 2)
			isMe := false
			for _, n := range tg.ownNames {
				if cmdWords[1] == n {
					isMe = true
					break
				}
			}
			if !isMe {
				return
			}
			cmdStr = cmdWords[0]
		}
		cmd := tg.Commands[cmdStr]
		if cmd == nil && strings.ContainsRune(cmdStr, '_') {
			words = strings.Split(cmdStr, "_")
			if len(words) > 0 {
				cmdStr = words[0]
			}
			if len(words) > 1 {
				args = append(args, words[1:]...)
			}
			cmd = tg.Commands[cmdStr]
		}
		if cmd == nil {
			tg.logger.Warning("Command not found:", cmdStr, "chat: ", chat)
			tg.SendMsg(tg.Messages.Commands.Unknown, []int64{chat}, false)
		} else {
			l := len(args)
			if l > 0 {
				sanitizedArgs := make([]string, 0, l)
				for _, a := range args {
					a = strings.TrimSpace(a)
					if len(a) > 0 {
						sanitizedArgs = append(sanitizedArgs, a)
					}
				}
				args = sanitizedArgs
			}
			if err := cmd(chat, cmdStr, args); err != nil {
				tg.logger.Error(err)
				tg.SendMsg(tg.Messages.Error+err.Error(), []int64{chat}, false)
			}
		}
	}

}

func (tg *Telegram) ValidateOTP(otp string) bool {
	var res bool
	if tg.totp != nil {
		res = tg.totp.Verify(otp, time.Now().Unix())
	} else {
		tg.logger.Warning("TOTP not initialised")
	}
	return res
}

func (tg *Telegram) AddCommand(cmd string, cmdFunc CommandFunc) error {
	var err error
	if cmd == "" || cmdFunc == nil {
		err = errors.New("unable to add empty command")
	} else if tg.Commands == nil {
		tg.Commands = make(map[string]CommandFunc)
	}
	tg.Commands[cmd] = cmdFunc
	return err
}

func (tg *Telegram) RmCommand(cmd string) {
	delete(tg.Commands, cmd)
}

func (tg *Telegram) startMessagesListener(closeChan <-chan bool) {
	tg.stopWaiter.Add(1)
	defer tg.stopWaiter.Done()
	listener := tg.Client.GetListener()
	defer listener.Close()
	for {
		select {
		case up := <-listener.Updates:
			if up != nil && up.GetClass() == mt.ClassUpdate && up.GetType() == mt.TypeUpdateNewMessage {
				upMsg := up.(*mt.UpdateNewMessage)
				if msg := upMsg.Message; msg != nil && !msg.IsOutgoing && msg.Content != nil &&
					msg.Content.MessageContentType() == mt.TypeMessageText {
					content := upMsg.Message.Content.(*mt.MessageText)
					if content.Text != nil && len(content.Text.Text) > 0 && content.Text.Text[0] == '/' {
						tg.logger.Debug("Got new message:", upMsg.Message)
						go func(msg *mt.Message) {
							tg.stopWaiter.Add(1)
							defer tg.stopWaiter.Done()
							tg.processCommand(msg)
						}(upMsg.Message)
					}
				}
			}
		case <-closeChan:
			return
		}
	}
}

func (tg *Telegram) startFileUploadListener(closeChan <-chan bool) {
	tg.stopWaiter.Add(1)
	defer tg.stopWaiter.Done()
	listener := tg.Client.GetListener()
	defer listener.Close()
	for {
		select {
		case up := <-listener.Updates:
			if up != nil && up.GetClass() == mt.ClassUpdate && up.GetType() == mt.TypeUpdateFile {
				updateMsg := up.(*mt.UpdateFile)
				if updateMsg != nil && updateMsg.File != nil && updateMsg.File.Remote != nil {
					if updateMsg.File.Remote.IsUploadingCompleted {
						tg.logger.Debug("File upload complete ", updateMsg.File.Remote.Id)
						if updateMsg.File.Local != nil {
							if absPath, err := filepath.Abs(updateMsg.File.Local.Path); err == nil {
								if callbacks, found := tg.fileCallbacks.LoadAndDelete(absPath); found {
									go func(callbacks []FileUploadCallback, path, id string) {
										tg.stopWaiter.Add(1)
										defer tg.stopWaiter.Done()
										for _, callback := range callbacks {
											callback(path, id)
										}
									}(callbacks.([]FileUploadCallback), absPath, updateMsg.File.Remote.Id)
								} else {
									tg.logger.Debug("Callback for ", absPath, " not found")
								}
							} else {
								tg.logger.Error(err)
							}
						} else {
							tg.logger.Warning("Unable to determine local file for ", updateMsg.File.Remote.Id)
						}
					} else {
						tg.logger.Debug("File uploading ",
							updateMsg.File.Remote.UploadedSize,
							" of ",
							updateMsg.File.Size, "bytes",
						)
					}
				}
			}
		case <-closeChan:
			return
		}
	}
}

func (tg *Telegram) startAuthClosedListener(closeChan chan bool) {
	tg.stopWaiter.Add(1)
	defer tg.stopWaiter.Done()
	listener := tg.Client.GetListener()
	defer listener.Close()
	for up := range listener.Updates {
		if up != nil && up.GetClass() == mt.ClassUpdate && up.GetType() == mt.TypeUpdateAuthorizationState {
			upState := up.(*mt.UpdateAuthorizationState)
			if upState.AuthorizationState.AuthorizationStateType() == mt.TypeAuthorizationStateClosed {
				close(closeChan)
				return
			}
		}
	}
}

func (tg *Telegram) HandleUpdates() {
	closeChan := make(chan bool)
	go tg.startMessagesListener(closeChan)
	go tg.startFileUploadListener(closeChan)
	tg.startAuthClosedListener(closeChan)
}

func (tg *Telegram) SendMsg(msgText string, chatIds []int64, formatted bool) {
	if msgText != "" && len(chatIds) > 0 {
		tg.logger.Debug("Sending message ", msgText, " to ", chatIds)
		var err error
		var fMsg *mt.FormattedText
		if formatted {
			fMsg = FormatText(msgText)
		} else {
			fMsg = &mt.FormattedText{Text: msgText}
		}
		msg := mt.InputMessageText{
			Text:               fMsg,
			LinkPreviewOptions: &mt.LinkPreviewOptions{IsDisabled: true},
			ClearDraft:         true,
		}
		for _, chatId := range chatIds {
			var title []string
			if title, err = tg.GetChatTitle(chatId); err != nil {
				tg.logger.Warning(err)
				title = append(title, strconv.FormatInt(chatId, 10))
			}
			tg.logger.Debug("Sending message to ", title, " id ", chatId)
			req := &mt.SendMessageRequest{
				ChatId:              chatId,
				InputMessageContent: &msg,
			}
			if _, err = tg.Client.SendMessage(req); err == nil {
				tg.logger.Debug("Message to ", title, " has been sent")
			}
			if err != nil {
				tg.logger.Error(err)
			}
		}
	}
}

func (tg *Telegram) sendMediaMessage(chatIds []int64,
	content mt.InputMessageContent,
	fileToWatch string,
	idSetter FileUploadCallback,
	uploadCallback FileUploadCallback) {
	absPath, err := filepath.Abs(fileToWatch)

	if err != nil {
		tg.logger.Error(err)
		return
	}

	sendFn := func(chat int64) {
		var err error
		var title []string
		if title, err = tg.GetChatTitle(chat); err != nil {
			tg.logger.Warning(err)
			title = append(title, strconv.FormatInt(chat, 10))
		}
		tg.logger.Debug("Sending message to ", title, " id ", chat)
		req := &mt.SendMessageRequest{
			ChatId:              chat,
			InputMessageContent: content,
		}
		if _, err = tg.Client.SendMessage(req); err != nil {
			tg.logger.Error(err)
		}
	}

	var callbacks []FileUploadCallback
	if len(chatIds) > 1 {
		callbacks = append(callbacks, idSetter, func(string, string) {
			for _, chatId := range chatIds[1:] {
				sendFn(chatId)
			}
		})
	}

	if uploadCallback != nil {
		callbacks = append(callbacks, uploadCallback)
	}

	if len(callbacks) > 0 {
		tg.logger.Debug("Setting file watch for ", absPath)
		tg.fileCallbacks.Store(absPath, callbacks)
	}

	sendFn(chatIds[0])
}

func (tg *Telegram) SendPhotoCallback(photo MediaParams,
	msgText string,
	chatIds []int64,
	formatted bool,
	callback FileUploadCallback) {
	if len(chatIds) > 0 {
		if len(photo.Path) == 0 {
			tg.logger.Warning("Photo is empty, sending as text")
			tg.SendMsg(msgText, chatIds, formatted)
		} else {
			tg.logger.Debug("Sending photo message ", msgText, " to ", chatIds)
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
			idSetter := func(_, id string) {
				msg.Photo = &mt.InputFileRemote{Id: id}
			}
			tg.sendMediaMessage(chatIds, msg, photo.Path, idSetter, callback)
		}
	}
}

func (tg *Telegram) SendPhoto(photo MediaParams, msgText string, chatIds []int64, formatted bool) {
	ch := make(chan bool)
	tg.SendPhotoCallback(photo, msgText, chatIds, formatted, func(string, string) {
		ch <- true
	})
	<-ch
}

func (tg *Telegram) SendVideoCallback(video MediaParams,
	msgText string,
	chatIds []int64,
	formatted bool,
	callback FileUploadCallback) {
	if len(chatIds) > 0 {
		if len(video.Path) == 0 {
			tg.logger.Warning("Video is empty, sending as text")
			tg.SendMsg(msgText, chatIds, formatted)
		} else {
			tg.logger.Debug("Sending video message ", msgText, " to ", chatIds)
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
			idSetter := func(_, id string) {
				msg.Video = &mt.InputFileRemote{Id: id}
			}
			tg.sendMediaMessage(chatIds, msg, video.Path, idSetter, callback)
		}
	}
}

func (tg *Telegram) SendVideo(video MediaParams, msgText string, chatIds []int64, formatted bool) {
	ch := make(chan bool)
	tg.SendVideoCallback(video, msgText, chatIds, formatted, func(string, string) {
		ch <- true
	})
	<-ch
}

func (tg *Telegram) GetChatTitle(id int64) (names []string, err error) {
	var chat *mt.Chat
	if chat, err = tg.Client.GetChat(&mt.GetChatRequest{ChatId: id}); err == nil {
		names = append(names, chat.Title)
	} else {
		tg.logger.Warning(err)
		var user *mt.User
		if user, err = tg.Client.GetUser(&mt.GetUserRequest{UserId: id}); err == nil {
			names = user.Usernames.ActiveUsernames
		}
	}
	return
}

func (tg *Telegram) GetChats() ([]int64, error) {
	var err error
	allChats := make([]int64, 0, 50)
	var chats *mt.Chats
	loadReq := &mt.LoadChatsRequest{
		Limit: chatsPageNum,
	}
	if _, err = tg.Client.LoadChats(loadReq); err == nil {
		getReq := &mt.GetChatsRequest{
			Limit: chatsPageNum,
		}
		if chats, err = tg.Client.GetChats(getReq); err == nil && chats != nil {
			allChats = append(allChats, chats.ChatIds...)
		} else {
			allChats = nil
		}
	}
	return allChats, err
}

func (tg *Telegram) LoginAsBot(botToken string, logLevel int32) error {
	var err error
	auth := mt.BotAuthorizer(botToken)
	auth.TdlibParameters <- tg.mtParameters
	logLevelRequest := mt.WithLogVerbosity(&mt.SetLogVerbosityLevelRequest{NewVerbosityLevel: logLevel})
	if tg.Client, err = mt.NewClient(auth, logLevelRequest); err == nil {
		var me *mt.User
		if me, err = tg.Client.GetMe(); err == nil {
			tg.ownNames = me.Usernames.ActiveUsernames
			tg.logger.Info("Authorized as", tg.ownNames)
		}
	}
	return err
}

func (tg *Telegram) LoginAsUser(inputHandler func(string) (string, error), logLevel int32) error {
	var err, authErr error
	auth := mt.ClientAuthorizer()
	go func() {
		for state := range auth.State {
			if state == nil {
				return
			}
			stateType := state.AuthorizationStateType()
			tg.logger.Info(stateType)
			var inputChan chan string
			switch stateType {
			case mt.TypeAuthorizationStateClosed, mt.TypeAuthorizationStateClosing, mt.TypeAuthorizationStateLoggingOut:
				authErr = errors.New("connection closing " + stateType)
				return
			case mt.TypeAuthorizationStateWaitTdlibParameters:
				auth.TdlibParameters <- tg.mtParameters
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
	}()
	logLevelRequest := mt.WithLogVerbosity(&mt.SetLogVerbosityLevelRequest{NewVerbosityLevel: logLevel})
	if tg.Client, err = mt.NewClient(auth, logLevelRequest); err == nil {
		if authErr != nil {
			err = authErr
		} else {
			var me *mt.User
			if me, err = tg.Client.GetMe(); err == nil {
				tg.ownNames = me.Usernames.ActiveUsernames
				tg.logger.Info("Authorized as", tg.ownNames)
				if _, err := tg.GetChats(); err != nil {
					tg.logger.Warning(err)
				}
			}
		}
	}
	return err
}

func (tg *Telegram) Close() {
	tg.onceCloser.Do(func() {
		if tg.Client != nil {
			if _, err := tg.Client.Close(); err != nil {
				tg.logger.Warning(err)
			}
		}
		tg.stopWaiter.Wait()
	})
}

func (tg *Telegram) SetLogger(logger TgLogger) {
	if logger == nil {
		tg.logger = silentLogger
	} else {
		tg.logger = logger
	}
}

func New(apiId int32, apiHash, dbLocation, filesLocation, otpSeed string) *Telegram {
	var totp *gotp.TOTP
	if len(otpSeed) > 0 {
		totp = gotp.NewDefaultTOTP(otpSeed)
	}
	tg := &Telegram{
		Commands: make(map[string]CommandFunc),
		mtParameters: &mt.SetTdlibParametersRequest{
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
			ApplicationVersion:     AppVersion,
			EnableStorageOptimizer: false,
			IgnoreFileNames:        false,
		},
		totp:   totp,
		logger: defaultLogger,
	}
	_ = tg.AddCommand(cmdStart, tg.startF)
	_ = tg.AddCommand(cmdAttach, tg.attachF)
	_ = tg.AddCommand(cmdDetach, tg.detachF)
	_ = tg.AddCommand(cmdSetAdmin, tg.setAdminF)
	_ = tg.AddCommand(cmdRmAdmin, tg.rmAdminF)
	_ = tg.AddCommand(cmdState, tg.stateF)
	return tg
}
