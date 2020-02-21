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
	"encoding/json"
	"flag"
	"fmt"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
)

type Config struct {
	ApiId   string
	ApiHash string
	Otp     string
	Chats   []int64
	Text    string
	Image   string
	Video   string
	Msg     MTHelper.TGMessages
}

func main() {
	flag.Parse()
	args := flag.Args()
	logger := logging.MustGetLogger("main")
	if len(args) == 0 {
		logger.Fatal("argument not set")
	}
	confData, err := ioutil.ReadFile(args[0])
	if err == nil {
		conf := Config{}
		if err = json.Unmarshal(confData, &conf); err == nil {
			MTHelper.SetupMtLog("test/mt.log", MTHelper.MtLogWarning)
			tg := MTHelper.New(conf.ApiId, conf.ApiHash, "test/dbdir", "test/fdir", conf.Otp)
			tg.Messages = conf.Msg
			var authFunc func(input string) string
			if len(args) > 1 && args[1] == "--auth" {
				authFunc = func(input string) string {
					fmt.Println(input)
					var res string
					if _, err := fmt.Scanln(&res); err != nil {
						logger.Error(err)
					}
					return res
				}
			}
			if err = tg.Login(authFunc, -1); err == nil {
				tg.HandleUpdates()
				tg.SendMsg(conf.Text, conf.Chats, true)
				tg.SendPhoto(MTHelper.MediaParams{
					Path:   conf.Image,
					Width:  0,
					Height: 0,
				}, conf.Text, conf.Chats, true)
				tg.SendVideo(MTHelper.MediaParams{
					Path:      conf.Video,
					Width:     0,
					Height:    0,
					Streaming: true,
				}, conf.Text, conf.Chats, true)
			}
		}
	}
	if err != nil {
		logger.Fatal(err)
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		os.Exit(0)
	}()
	<-ch
}
