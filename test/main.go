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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/op/go-logging"

	"sot-te.ch/MTHelper"
)

type Config struct {
	ApiId    int32
	ApiHash  string
	BotToken string
	Otp      string
	Chats    []int64
	Text     string
	Image    string
	Video    string
	Msg      mthelper.TGMessages
}

func main() {
	flag.Parse()
	args := flag.Args()
	logger := logging.MustGetLogger("main")
	if len(args) == 0 {
		logger.Fatal("argument not set")
	}
	confData, err := os.ReadFile(args[0])
	if err == nil {
		conf := Config{}
		if err = json.Unmarshal(confData, &conf); err == nil {
			tg := mthelper.New(conf.ApiId, conf.ApiHash, "test/dbdir", "test/fdir", conf.Otp)
			defer func() {
				tg.Close()
			}()
			tg.Messages = conf.Msg

			if len(conf.BotToken) > 0 {
				err = tg.LoginAsBot(conf.BotToken, mthelper.MtLogWarning)
			} else {
				err = tg.LoginAsUser(func(in string) (string, error) {
					var err error
					var out string
					fmt.Println(in)
					_, err = fmt.Scanln(&out)
					return out, err
				}, mthelper.MtLogWarning)
			}
			if err == nil {
				chats, _ := tg.GetChats()
				logger.Info(chats)
				go tg.HandleUpdates()
				tg.SendMsg(conf.Text, conf.Chats, true)
				logger.Info("Message sent")
				//tg.SendPhotoCallback(MTHelper.MediaParams{
				//	Path:   conf.Image,
				//	Width:  0,
				//	Height: 0,
				//}, conf.Text, conf.Chats, true, nil)
				//logger.Info("Photo sent")
				//tg.SendVideoCallback(MTHelper.MediaParams{
				//	Path:      conf.Video,
				//	Width:     0,
				//	Height:    0,
				//	Streaming: true,
				//}, conf.Text, conf.Chats, true, nil)
				//logger.Info("Video sent")
			}
		}
	}
	if err != nil {
		logger.Fatal(err)
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}
