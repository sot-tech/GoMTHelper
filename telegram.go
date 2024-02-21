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

// Package mthelper used to create simple Telegram bot with native TD library wrapper
package mthelper

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
	nc "github.com/zelenin/go-tdlib/client"
)

var (
	// DebugLogger prints all messages into stdout
	DebugLogger = logging.MustGetLogger("sot-te.ch/MTHelper")
	// AppVersion used while new Telegram instance creation
	AppVersion = "0.295582a"
)

// TdLogLevel used by native telegram client
type TdLogLevel int32

func (ll TdLogLevel) buildOption() nc.Option {
	return nc.WithLogVerbosity(&nc.SetLogVerbosityLevelRequest{NewVerbosityLevel: int32(ll)})
}

// Native TD log levels
const (
	MtLogFatal TdLogLevel = iota
	MtLogError
	MtLogWarning
	MtLogInfo
	MtLogDebug
	MtLogVerbose
)

// Constants for telegram bot commands
const (
	CmdStart    = "/start"
	CmdAttach   = "/attach"
	CmdDetach   = "/detach"
	CmdSetAdmin = "/setadmin"
	CmdRmAdmin  = "/rmadmin"
	CmdState    = "/state"
)

// MessageMaxLen message max length.
// If message content is longer than this constant, it will be discarded.
const MessageMaxLen = 1000

const chatsPageNum int32 = math.MaxInt32

// TgLogger abstract logger interface for MTHelper.
// Custom logger can be set for specific Telegram instance.
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

// SilentLogger discards all messages
var SilentLogger TgLogger = nopLogger{}

// CommandFunc function prototype for message/command processing.
// chatId - is ID of other party, who sent message to instance.
// Message will be separated to cmd (first token) and args (other tokens) with
// space and dash (_) separators.
// If message content is longer than MessageMaxLen or if
// cmd is `/command@Name`-like and `Name` is not one of user's/bot's name,
// entire message will be discarded before CommandFunc call.
// If CommandFunc returns non-nil error, error text will be sent to chat.
type CommandFunc func(chatId int64, cmd string, args []string) error

// FileUploadCallback function prototype for executing after media message
// (Telegram.SendPhotoCallback and Telegram.SendVideoCallback) will be sent to all participants.
// path - is absolute path of uploaded local file, id - uploaded file's telegram ID.
type FileUploadCallback func(path, id string)

// TGBackendFunction structure, which contains functions,
// used for chats operating, such as storing/deleting some
// as regular or administrator.
// All functions MUST be concurrent-safe.
type TGBackendFunction struct {
	// ChatExist checks if store contains chat added by CmdAttach
	ChatExist func(int64) (bool, error)
	// ChatAdd function to execute on CmdAttach command
	ChatAdd func(int64) error
	// ChatRm function to execute on CmdDetach command
	ChatRm func(int64) error
	// AdminExist checks if store contains chat added by CmdSetAdmin.
	AdminExist func(int64) (bool, error)
	// AdminAdd function to execute on CmdSetAdmin command.
	// Works only if TOTP set up and chat send correct value.
	// Should grant administrator rights to provided chat ID.
	AdminAdd func(int64) error
	// AdminRm function to execute on CmdRmAdmin command.
	// Should revoke administrator rights from provided chat ID.
	AdminRm func(int64) error
	// State function to execute on CmdState command.
	// Should report state/role of chat.
	State func(int64) (string, error)
}

// TGCommandResponse set of static responses to commands from TGBackendFunction
type TGCommandResponse struct {
	// Start static response to CmdStart command
	Start string `json:"start"`
	// Attach static message to CmdAttach command if TGBackendFunction.ChatAdd executed successfully
	Attach string `json:"attach"`
	// Detach static message to CmdDetach command if TGBackendFunction.ChatRm executed successfully
	Detach string `json:"detach"`
	// SetAdmin static message to CmdSetAdmin command if TGBackendFunction.AdminAdd executed successfully
	SetAdmin string `json:"setadmin"`
	// RmAdmin static message to CmdRmAdmin command if TGBackendFunction.AdminRm executed successfully
	RmAdmin string `json:"rmadmin"`
	// Unknown static message which will be sent if provided command is unknown
	Unknown string `json:"unknown"`
}

// TGMessages is extended structure to command responses
type TGMessages struct {
	Commands TGCommandResponse `json:"cmds"`
	// Unauthorized static message which will be sent if chat is not authorized
	// to execute command i.e. CmdRmAdmin called from chat which is not in admin list
	// or CmdSetAdmin - if OTP validation failed
	Unauthorized string `json:"auth"`
	// Error static message prefix, which will be sent if error occurred.
	Error string `json:"error"`
}

// MediaParams structure used for image/video sending.
// See Telegram.SendPhotoCallback, Telegram.SendVideoCallback.
type MediaParams struct {
	// Path to file to be sent to telegram. Must exist before call.
	Path string
	// Width and Height are dimensions of media file.
	// If not provided, some clients will show black square instead of preview
	Width, Height int32
	// Streaming flag if media content can be streamed
	// (i.e. play video without saving it locally).
	Streaming bool
	// Thumbnail is parameters for video thumbnail image.
	Thumbnail *MediaParams
}

// Telegram is utility structure for native TD client wrapper.
type Telegram struct {
	// Client is direct interface for telegram wrapper
	Client   *nc.Client
	clientMu sync.Mutex
	// Structure, containing message responses
	Messages TGMessages
	// Commands - map of command handlers.
	// Keys are slash-prefixed commands (/start, /help...).
	// See CommandFunc specification.
	Commands         map[string]CommandFunc
	commandsMu       sync.RWMutex
	BackendFunctions TGBackendFunction
	// TdParameters parameters, used for native client creation
	TdParameters *nc.SetTdlibParametersRequest
	// TOTP used to validate privileged commands.
	// If not set, validation disabled.
	TOTP *gotp.TOTP
	// Logger internal logger.
	// Utility will panic if nil, provide at least SilentLogger
	Logger                TgLogger
	fileCallbacks         sync.Map
	fileCallbacksOutdated sync.Map
	ownNames              []string
	stopWaiter            sync.WaitGroup
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
			tg.Logger.Notice("New chat added ", chat)
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
			tg.Logger.Notice("Chat deleted ", chat)
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
				tg.Logger.Notice("New admin added ", chat)
				tg.SendMsg(tg.Messages.Commands.SetAdmin, []int64{chat}, false)
			}
		} else {
			tg.Logger.Info("SetAdmin unauthorized ", chat)
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
					tg.Logger.Notice("Admin deleted ", chat)
					tg.SendMsg(tg.Messages.Commands.RmAdmin, []int64{chat}, false)
				}
			} else {
				tg.Logger.Info("RmAdmin unauthorized ", chat)
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

func (tg *Telegram) processCommand(msg *nc.Message) {
	chat := msg.ChatId
	content := msg.Content.(*nc.MessageText)
	if content != nil && content.Text != nil && len(content.Text.Text) <= MessageMaxLen {
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
		tg.commandsMu.RLock()
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
		tg.commandsMu.RUnlock()
		if cmd == nil {
			tg.Logger.Warning("Command not found:", cmdStr, "chat: ", chat)
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
				tg.Logger.Error(err)
				tg.SendMsg(tg.Messages.Error+err.Error(), []int64{chat}, false)
			}
		}
	}
}

// ValidateOTP checks if provided value equals to code, generated by TOTP.
// Function also will return false is TOTP not configured for this Telegram instance.
func (tg *Telegram) ValidateOTP(value string) bool {
	var res bool
	if tg.TOTP != nil {
		res = tg.TOTP.Verify(value, time.Now().Unix())
	} else {
		tg.Logger.Warning("TOTP not initialised")
	}
	return res
}

// ErrEmptyCommand indicates, that command of cmdFunc, passed to Telegram.AddCommand
// is empty or nil
var ErrEmptyCommand = errors.New("unable to add empty command")

// AddCommand registers command handler.
// Every call of cmdFunc will be in separate goroutine.
// See CommandFunc specification.
// Function is concurrent-safe.
func (tg *Telegram) AddCommand(command string, cmdFunc CommandFunc) error {
	var err error
	tg.commandsMu.Lock()
	if command == "" || cmdFunc == nil {
		err = ErrEmptyCommand
	} else if tg.Commands == nil {
		tg.Commands = make(map[string]CommandFunc)
	}
	tg.Commands[command] = cmdFunc
	tg.commandsMu.Unlock()
	return err
}

// RmCommand removes handler for command.
// Function is concurrent-safe.
func (tg *Telegram) RmCommand(command string) {
	tg.commandsMu.Lock()
	delete(tg.Commands, command)
	tg.commandsMu.Unlock()
}

func (tg *Telegram) startMessagesListener(closeChan <-chan bool) {
	tg.stopWaiter.Add(1)
	defer tg.stopWaiter.Done()
	listener := tg.Client.GetListener()
	defer listener.Close()
	for {
		select {
		case up := <-listener.Updates:
			if up != nil && up.GetClass() == nc.ClassUpdate && up.GetType() == nc.TypeUpdateNewMessage {
				upMsg := up.(*nc.UpdateNewMessage)
				if msg := upMsg.Message; msg != nil && !msg.IsOutgoing && msg.Content != nil &&
					msg.Content.MessageContentType() == nc.TypeMessageText {
					content := upMsg.Message.Content.(*nc.MessageText)
					if content.Text != nil && len(content.Text.Text) > 0 && content.Text.Text[0] == '/' {
						tg.Logger.Debug("Got new message:", upMsg.Message)
						tg.stopWaiter.Add(1)
						go func(msg *nc.Message) {
							defer tg.stopWaiter.Done()
							tg.processCommand(msg)
						}(upMsg.Message)
					}
				}
			}
		case <-closeChan:
			tg.Logger.Debug("Message listener stopped")
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
			if up != nil && up.GetClass() == nc.ClassUpdate && up.GetType() == nc.TypeUpdateFile {
				updateMsg := up.(*nc.UpdateFile)
				if updateMsg != nil &&
					updateMsg.File != nil &&
					updateMsg.File.Remote != nil &&
					updateMsg.File.Local != nil {
					if updateMsg.File.Remote.IsUploadingCompleted {
						tg.Logger.Debug("File upload complete ", updateMsg.File.Remote.Id)
						if updateMsg.File.Local != nil {
							if absPath, err := filepath.Abs(updateMsg.File.Local.Path); err == nil {
								if callbacks, found := tg.fileCallbacks.LoadAndDelete(absPath); found {
									tg.fileCallbacksOutdated.Store(absPath, true)
									tg.stopWaiter.Add(1)
									go func(callbacks []FileUploadCallback, path, id string) {
										defer tg.stopWaiter.Done()
										tg.Logger.Debug("Executing ", len(callbacks), " callbacks for file ", path)
										for _, callback := range callbacks {
											callback(path, id)
										}
									}(callbacks.([]FileUploadCallback), absPath, updateMsg.File.Remote.Id)
								} else if _, found = tg.fileCallbacksOutdated.LoadAndDelete(absPath); !found {
									tg.Logger.Warning("Callbacks for ", absPath, " not registered")
								}
							} else {
								tg.Logger.Error(err)
							}
						} else {
							tg.Logger.Warning("Unable to determine local file for ", updateMsg.File.Remote.Id)
						}
					} else {
						tg.Logger.Debug("File ",
							updateMsg.File.Local.Path,
							" uploading: ",
							updateMsg.File.Remote.UploadedSize,
							" of ",
							updateMsg.File.Size, "bytes",
						)
					}
				}
			}
		case <-closeChan:
			tg.Logger.Debug("File upload listener stopped")
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
		if up != nil && up.GetClass() == nc.ClassUpdate && up.GetType() == nc.TypeUpdateAuthorizationState {
			upState := up.(*nc.UpdateAuthorizationState)
			if upState.AuthorizationState.AuthorizationStateType() == nc.TypeAuthorizationStateClosed {
				tg.Logger.Debug("Received auth closed event")
				close(closeChan)
				return
			}
		}
	}
}

// HandleUpdates starts messages/command listener.
// Every registered CommandFunc
// Function will block goroutine until Close called.
func (tg *Telegram) HandleUpdates() {
	closeChan := make(chan bool)
	go tg.startMessagesListener(closeChan)
	go tg.startFileUploadListener(closeChan)
	tg.startAuthClosedListener(closeChan)
}

// SendMsg sends text message to provided chatIds.
// If formatted flag checked, msgText will be parsed as markdown.
// Function will block goroutine until message will be sent to all chatIds.
func (tg *Telegram) SendMsg(msgText string, chatIDs []int64, formatted bool) {
	if msgText != "" && len(chatIDs) > 0 {
		tg.Logger.Debug("Sending message ", msgText, " to ", chatIDs)
		var err error
		var fMsg *nc.FormattedText
		if formatted {
			fMsg = FormatText(msgText)
		} else {
			fMsg = &nc.FormattedText{Text: msgText}
		}
		msg := nc.InputMessageText{
			Text:               fMsg,
			LinkPreviewOptions: &nc.LinkPreviewOptions{IsDisabled: true},
			ClearDraft:         true,
		}
		for _, chatID := range chatIDs {
			var title []string
			if title, err = tg.GetChatTitle(chatID); err != nil {
				tg.Logger.Warning(err)
				title = append(title, strconv.FormatInt(chatID, 10))
			}
			tg.Logger.Debug("Sending message to ", title, " id ", chatID)
			req := &nc.SendMessageRequest{
				ChatId:              chatID,
				InputMessageContent: &msg,
			}
			if _, err = tg.Client.SendMessage(req); err == nil {
				tg.Logger.Debug("Message to ", title, " has been sent")
			}
			if err != nil {
				tg.Logger.Error(err)
			}
		}
	}
}

func (tg *Telegram) sendMediaMessage(chatIDs []int64,
	content nc.InputMessageContent,
	fileToWatch string,
	idSetter FileUploadCallback,
	uploadCallback FileUploadCallback,
) {
	absPath, err := filepath.Abs(fileToWatch)
	if err != nil {
		tg.Logger.Error(err)
		return
	}

	sendFn := func(chat int64) {
		var err error
		var title []string
		if title, err = tg.GetChatTitle(chat); err != nil {
			tg.Logger.Warning(err)
			title = append(title, strconv.FormatInt(chat, 10))
		}
		tg.Logger.Debug("Sending message to ", title, " id ", chat)
		req := &nc.SendMessageRequest{
			ChatId:              chat,
			InputMessageContent: content,
		}
		if _, err = tg.Client.SendMessage(req); err != nil {
			tg.Logger.Error(err)
		}
	}

	var callbacks []FileUploadCallback
	if len(chatIDs) > 1 {
		callbacks = append(callbacks, idSetter, func(string, string) {
			for _, chatID := range chatIDs[1:] {
				sendFn(chatID)
			}
		})
	}

	if uploadCallback != nil {
		callbacks = append(callbacks, uploadCallback)
	}

	if len(callbacks) > 0 {
		tg.Logger.Debug("Setting file watch for ", absPath)
		tg.fileCallbacks.Store(absPath, callbacks)
	}

	sendFn(chatIDs[0])
}

// SendPhotoCallback sends photo/image to provided chatIds with msgText caption.
// If formatted flag checked, msgText will be parsed as markdown.
// Function works asynchronous and will execute callback (if any) after
// message will be sent to all chatIds.
func (tg *Telegram) SendPhotoCallback(photo MediaParams,
	msgText string,
	chatIds []int64,
	formatted bool,
	callback FileUploadCallback,
) {
	if len(chatIds) > 0 {
		if len(photo.Path) == 0 {
			tg.Logger.Warning("Photo is empty, sending as text")
			tg.SendMsg(msgText, chatIds, formatted)
		} else {
			tg.Logger.Debug("Sending photo message ", msgText, " to ", chatIds)
			var caption *nc.FormattedText
			var thumbnail *nc.InputThumbnail
			if formatted {
				caption = FormatText(msgText)
			} else {
				caption = &nc.FormattedText{Text: msgText}
			}
			if photo.Thumbnail != nil && len(photo.Thumbnail.Path) > 0 {
				thumbnail = &nc.InputThumbnail{
					Thumbnail: &nc.InputFileLocal{Path: photo.Thumbnail.Path},
					Width:     photo.Thumbnail.Width,
					Height:    photo.Thumbnail.Height,
				}
			}
			msg := &nc.InputMessagePhoto{
				Photo:     &nc.InputFileLocal{Path: photo.Path},
				Thumbnail: thumbnail,
				Width:     photo.Width,
				Height:    photo.Height,
				Caption:   caption,
			}
			idSetter := func(_, id string) {
				msg.Photo = &nc.InputFileRemote{Id: id}
			}
			tg.sendMediaMessage(chatIds, msg, photo.Path, idSetter, callback)
		}
	}
}

// SendPhoto sends photo/image to provided chatIds with msgText caption.
// If formatted flag checked, msgText will be parsed as markdown.
// Function will block goroutine until message will be sent to all chatIds.
func (tg *Telegram) SendPhoto(photo MediaParams, msgText string, chatIds []int64, formatted bool) {
	ch := make(chan bool)
	tg.SendPhotoCallback(photo, msgText, chatIds, formatted, func(string, string) {
		close(ch)
	})
	<-ch
}

// SendVideoCallback sends video to provided chatIds with msgText caption.
// If formatted flag checked, msgText will be parsed as markdown.
// Function works asynchronous and will execute callback (if any) after
// message will be sent to all chatIds.
func (tg *Telegram) SendVideoCallback(video MediaParams,
	msgText string,
	chatIds []int64,
	formatted bool,
	callback FileUploadCallback,
) {
	if len(chatIds) > 0 {
		if len(video.Path) == 0 {
			tg.Logger.Warning("Video is empty, sending as text")
			tg.SendMsg(msgText, chatIds, formatted)
		} else {
			tg.Logger.Debug("Sending video message ", msgText, " to ", chatIds)
			var caption *nc.FormattedText
			var thumbnail *nc.InputThumbnail
			if formatted {
				caption = FormatText(msgText)
			} else {
				caption = &nc.FormattedText{Text: msgText}
			}
			if video.Thumbnail != nil && len(video.Thumbnail.Path) > 0 {
				thumbnail = &nc.InputThumbnail{
					Thumbnail: &nc.InputFileLocal{Path: video.Thumbnail.Path},
					Width:     video.Thumbnail.Width,
					Height:    video.Thumbnail.Height,
				}
			}
			msg := &nc.InputMessageVideo{
				Video:             &nc.InputFileLocal{Path: video.Path},
				Thumbnail:         thumbnail,
				Width:             video.Width,
				Height:            video.Height,
				SupportsStreaming: video.Streaming,
				Caption:           caption,
			}
			idSetter := func(_, id string) {
				msg.Video = &nc.InputFileRemote{Id: id}
			}
			tg.sendMediaMessage(chatIds, msg, video.Path, idSetter, callback)
		}
	}
}

// SendVideo sends video to provided chatIds with msgText caption.
// If formatted flag checked, msgText will be parsed as markdown.
// Function will block goroutine until message will be sent to all chatIds.
func (tg *Telegram) SendVideo(video MediaParams, msgText string, chatIds []int64, formatted bool) {
	ch := make(chan bool)
	tg.SendVideoCallback(video, msgText, chatIds, formatted, func(string, string) {
		close(ch)
	})
	<-ch
}

// GetChatTitle returns chat titles or usernames of provided chat id.
func (tg *Telegram) GetChatTitle(id int64) (names []string, err error) {
	var chat *nc.Chat
	if chat, err = tg.Client.GetChat(&nc.GetChatRequest{ChatId: id}); err == nil {
		names = append(names, chat.Title)
	} else {
		tg.Logger.Warning(err)
		var user *nc.User
		if user, err = tg.Client.GetUser(&nc.GetUserRequest{UserId: id}); err == nil {
			names = user.Usernames.ActiveUsernames
		}
	}
	return
}

// GetChats loads and returns list of known chat IDs.
// If current instance logged in as bot, returned list may be empty,
// due to telegram limitations.
func (tg *Telegram) GetChats() ([]int64, error) {
	var err error
	allChats := make([]int64, 0, 50)
	var chats *nc.Chats
	loadReq := &nc.LoadChatsRequest{
		Limit: chatsPageNum,
	}
	if _, err = tg.Client.LoadChats(loadReq); err == nil {
		getReq := &nc.GetChatsRequest{
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

// LoginAsBot creates new telegram client and logs in with bot credentials
// (bot token).
// logLevel used to limit messages from native TD library.
func (tg *Telegram) LoginAsBot(botToken string, logLevel TdLogLevel) error {
	var err error
	auth := nc.BotAuthorizer(botToken)
	tg.clientMu.Lock()
	defer tg.clientMu.Unlock()
	auth.TdlibParameters <- tg.TdParameters
	if tg.Client, err = nc.NewClient(auth, logLevel.buildOption()); err == nil {
		var me *nc.User
		if me, err = tg.Client.GetMe(); err == nil {
			tg.ownNames = me.Usernames.ActiveUsernames
			tg.Logger.Info("Authorized as", tg.ownNames)
		}
	}
	return err
}

// ErrAuthHandlerNotProvided indicates, that inputHandler, passed to Telegram.LoginAsUser is nil
var ErrAuthHandlerNotProvided = errors.New("authorization handler not provided")

// LoginAsUser creates new native client and logs in as regular user.
// inputHandler must return requested login data, such as:
// client.TypeAuthorizationStateWaitPhoneNumber, client.TypeAuthorizationStateWaitCode and
// client.TypeAuthorizationStateWaitPassword.
// If inputHandler returns error, this function will also return this error.
// logLevel used to limit messages from native TD library.
func (tg *Telegram) LoginAsUser(inputHandler func(string) (string, error), logLevel TdLogLevel) error {
	var err, authErr error
	auth := nc.ClientAuthorizer()
	tg.clientMu.Lock()
	defer tg.clientMu.Unlock()
	go func() {
		for state := range auth.State {
			if state == nil {
				return
			}
			stateType := state.AuthorizationStateType()
			tg.Logger.Info(stateType)
			var inputChan chan string
			switch stateType {
			case nc.TypeAuthorizationStateClosed, nc.TypeAuthorizationStateClosing, nc.TypeAuthorizationStateLoggingOut:
				authErr = errors.New("connection closing " + stateType)
				return
			case nc.TypeAuthorizationStateWaitTdlibParameters:
				auth.TdlibParameters <- tg.TdParameters
				continue
			case nc.TypeAuthorizationStateWaitPhoneNumber:
				inputChan = auth.PhoneNumber
			case nc.TypeAuthorizationStateWaitCode:
				inputChan = auth.Code
			case nc.TypeAuthorizationStateWaitPassword:
				inputChan = auth.Password
			case nc.TypeAuthorizationStateReady:
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
				authErr = ErrAuthHandlerNotProvided
				return
			}
		}
	}()
	if tg.Client, err = nc.NewClient(auth, logLevel.buildOption()); err == nil {
		if authErr != nil {
			err = authErr
		} else {
			var me *nc.User
			if me, err = tg.Client.GetMe(); err == nil {
				tg.ownNames = me.Usernames.ActiveUsernames
				tg.Logger.Info("Authorized as", tg.ownNames)
				if _, err := tg.GetChats(); err != nil {
					tg.Logger.Warning(err)
				}
			}
		}
	}
	return err
}

// Close stops native TD wrapper and message listener.
// Function will block goroutine until message listener
// and all started FileUploadCallback-s and CommandFunc-s complete execution.
func (tg *Telegram) Close() {
	tg.clientMu.Lock()
	defer tg.clientMu.Unlock()
	if tg.Client != nil {
		tg.Logger.Info("Closing telegram client")
		if _, err := tg.Client.Close(); err != nil {
			tg.Logger.Warning(err)
		}
	}
	tg.Logger.Debug("Waiting subroutines to stop")
	tg.stopWaiter.Wait()
	tg.Logger.Debug("Subroutines stopped")
}

// SetLogger registers logger for this Telegram instance.
// Provided logger will NOT be used by native TD client,
// to limit level of native client - pass logLevel to
// Telegram.LoginAsUser or Telegram.LoginAsBot functions.
// If logger is nil, all messages from Telegram will be discarded.
func (tg *Telegram) SetLogger(logger TgLogger) {
	if logger == nil {
		tg.Logger = SilentLogger
	} else {
		tg.Logger = logger
	}
}

// New creates new Telegram instance with provided parameters, DebugLogger and
// registered handlers for CmdStart, CmdAttach, CmdDetach, CmdSetAdmin, CmdRmAdmin and CmdState commands.
//
// apiId and apiHash - parameters, received from telegram application page: https://my.telegram.org/apps.
//
// dbLocation - path in filesystem where to store internal database.
// Should be in persistent store and may avoid providing regular
// user credentials if logged in before (Telegram.LoginAsUser needs to be called anyway).
//
// filesLocation - path in filesystem where to store files, if empty - defaults to current working directory.
// Mainly unused and can be placed to temp directory, if empty - defaults to current working directory.
//
// otpSeed is optional base32 seed.
// If it is not empty, new TOTP instance will be initialized
// with SHA1 algorithm, 6 code digits and 30 seconds interval.
func New(apiID int32, apiHash, dbLocation, filesLocation, otpSeed string) *Telegram {
	var totp *gotp.TOTP
	if len(otpSeed) > 0 {
		totp = gotp.NewDefaultTOTP(otpSeed)
	}
	tg := &Telegram{
		Commands: make(map[string]CommandFunc),
		TdParameters: &nc.SetTdlibParametersRequest{
			DatabaseDirectory:  dbLocation,
			FilesDirectory:     filesLocation,
			ApiId:              apiID,
			ApiHash:            apiHash,
			SystemLanguageCode: "en",
			DeviceModel:        runtime.GOOS,
			SystemVersion:      "n/d",
			ApplicationVersion: AppVersion,
		},
		TOTP:   totp,
		Logger: DebugLogger,
	}
	_ = tg.AddCommand(CmdStart, tg.startF)
	_ = tg.AddCommand(CmdAttach, tg.attachF)
	_ = tg.AddCommand(CmdDetach, tg.detachF)
	_ = tg.AddCommand(CmdSetAdmin, tg.setAdminF)
	_ = tg.AddCommand(CmdRmAdmin, tg.rmAdminF)
	_ = tg.AddCommand(CmdState, tg.stateF)
	return tg
}
