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

package main

import (
	MTHelper "MTPHelper"
	"fmt"
	mt "github.com/Arman92/go-tdlib"
	"math"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func authorize(client *mt.Client) {
	for {
		currentState, _ := client.Authorize()
		stateEnum := currentState.GetAuthorizationStateEnum()
		if stateEnum == mt.AuthorizationStateWaitPhoneNumberType {
			fmt.Print("Enter phone: ")
			var number string
			var err error
			if _, err = fmt.Scanln(&number); err == nil {
				_, err = client.SendPhoneNumber(number)
			}
			if err != nil {
				fmt.Printf("Error sending phone number: %v", err)
			}
		} else if stateEnum == mt.AuthorizationStateWaitCodeType {
			fmt.Print("Enter code: ")
			var code string
			var err error
			if _, err = fmt.Scanln(&code); err == nil {
				_, err = client.SendAuthCode(code)
			}
			if err != nil {
				fmt.Printf("Error sending auth code : %v", err)
			}
		} else if stateEnum == mt.AuthorizationStateWaitPasswordType {
			fmt.Print("Enter Password: ")
			var password string
			var err error
			if _, err = fmt.Scanln(&password); err == nil {
				_, err = client.SendAuthPassword(password)
			}
			if err != nil {
				fmt.Printf("Error sending auth password: %v", err)
			}
		} else if stateEnum == mt.AuthorizationStateReadyType {
			fmt.Println("Authorization Ready! Let's rock")
			break
		}
	}
}

const chatsPageNum int32 = 100

func getChats(client *mt.Client) ([]int64, error){
	var err error
	allChats := make([]int64, 0, 50)
	var chatIdOffset int64
	offsetOrder := mt.JSONInt64(math.MaxInt64)
	for{
		var chats *mt.Chats
		if chats, err = client.GetChats(offsetOrder, chatIdOffset, chatsPageNum); err == nil{
			if chats == nil || len(chats.ChatIDs) == 0{
				break
			}
			allChats = append(allChats, chats.ChatIDs...)
			chatIdOffset = allChats[len(allChats)-1]
			var chat *mt.Chat
			if chat, err = client.GetChat(chatIdOffset); err == nil{
				if chat != nil{
					offsetOrder = chat.Order
				}
			} else{
				allChats = nil
				break
			}
		} else{
			allChats = nil
			break
		}
	}
	return allChats, err
}



func sendMessage(client *mt.Client, chat int64, text string) (int64, error) {
	var err error
	var id int64
	msg := mt.NewInputMessageText(MTHelper.FormatText(text), true, true)
	if sentMessage, e := client.SendMessage(chat, 0, false, false, nil, msg);
		e != nil {
		fmt.Println(e.Error())
		err = e
	} else if sentMessage != nil {
		fmt.Println(sentMessage.ID)
		id = sentMessage.ID
	}
	return id, err
}

func main() {
	mt.SetLogVerbosityLevel(5)
	mt.SetFilePath("test/errors.log")
	client := mt.NewClient(mt.Config{
		APIID:                  "-",
		APIHash:                "-",
		SystemLanguageCode:     "en",
		DeviceModel:            runtime.GOOS,
		SystemVersion:          "n/d",
		ApplicationVersion:     "0.195284w",
		UseTestDataCenter:      false,
		DatabaseDirectory:      "test/dbdir",
		FileDirectory:          "test/fdir",
		UseFileDatabase:        false,
		UseChatInfoDatabase:    false,
		UseMessageDatabase:     false,
		UseSecretChats:         false,
		EnableStorageOptimizer: true,
		IgnoreFileNames:        false,
	})
	defer client.DestroyInstance()
	authorize(client)
	go func() {
		for update := range client.GetRawUpdatesChannel(0) {
			fmt.Println(update.Data)
		}
	}()
	if chats, err := getChats(client); err != nil {
		println(err.Error())
		os.Exit(1)
	} else{
		for _, chat := range chats {
			fmt.Printf("Chat: %d\n", chat)
		}
	}
	if state, err := client.GetAuthorizationState(); err == nil{
		fmt.Println(state)
	} else{
		fmt.Println(err)
	}
	_, _ = sendMessage(client, 1053007259, "**strong-text** \\* _curse-text_\n**_strongcurse-text_** **_`code-text`_** [**_strong-surse-link_**](http://some.site) ~~stroked-text~~\n[link-text](http://link.addr)\n```go\ncode\nblock\n```")
	//video := mt.NewInputMessageVideo(mt.NewInputFileLocal("/home/sot/Downloads/TEST.mp4"), nil, nil, 0, 1280, 720, true, mt.NewFormattedText("**TEST**", nil), 0)
	//if sentMessage, err := client.SendMessage(1053007259, 0, false, false, nil, video);
	//	err != nil {
	//	fmt.Println(err.Error())
	//} else if sentMessage != nil {
	//	fmt.Println(sentMessage.ID)
	//}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		os.Exit(0)
	}()
	<-ch
}
