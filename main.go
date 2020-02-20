package main

import (
	"bufio"
	"fmt"
	mt "github.com/Arman92/go-tdlib"
	md "github.com/russross/blackfriday"
	"io"
	"math"
	"os"
	"os/signal"
	"syscall"
)

func authorize(client *mt.Client) {
	inputScanner := bufio.NewScanner(os.Stdin)
	for {
		currentState, _ := client.Authorize()
		stateEnum := currentState.GetAuthorizationStateEnum()
		if stateEnum == mt.AuthorizationStateWaitPhoneNumberType {
			fmt.Print("Enter phone: ")
			var number string
			var err error
			if inputScanner.Scan() {
				if err = inputScanner.Err(); err == nil {
					_, err = client.SendPhoneNumber(number)
				}
			}

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

var allChats []*mt.Chat
var haveFullChatList bool

func getChatList(client *mt.Client, limit int) error {

	if !haveFullChatList && limit > len(allChats) {
		offsetOrder := int64(math.MaxInt64)
		offsetChatID := int64(0)
		var lastChat *mt.Chat

		if len(allChats) > 0 {
			lastChat = allChats[len(allChats)-1]
			offsetOrder = int64(lastChat.Order)
			offsetChatID = lastChat.ID
		}

		// get chats (ids) from tdlib
		chats, err := client.GetChats(mt.JSONInt64(offsetOrder),
			offsetChatID, int32(limit-len(allChats)))
		if err != nil {
			return err
		}
		if len(chats.ChatIDs) == 0 {
			haveFullChatList = true
			return nil
		}

		for _, chatID := range chats.ChatIDs {
			// get chat info from tdlib
			chat, err := client.GetChat(chatID)
			if err == nil {
				allChats = append(allChats, chat)
			} else {
				return err
			}
		}
		return getChatList(client, limit)
	}
	return nil
}

type TGRenderer struct{
	Index int
	Entities []mt.TextEntity
	openedEntities map[md.NodeType]*mt.TextEntity
}

func NewRenderer() *TGRenderer{
	return &TGRenderer{
		Index:          0,
		Entities:       make([]mt.TextEntity, 0, 10),
		openedEntities: make(map[md.NodeType]*mt.TextEntity),
	}
}

//TODO: finalize openedEntities

func (r *TGRenderer) RenderNode(w io.Writer, node *md.Node, entering bool) md.WalkStatus{
	s := string(node.Literal)
	l := len(s)
	if entering {
		var t mt.TextEntityType
		var extra string
		switch node.Type {
		case md.Strong:
			t = mt.NewTextEntityTypeBold()
		case md.Emph:
			t = mt.NewTextEntityTypeItalic()
		case md.Del:
			t = mt.NewTextEntityTypeCashtag()
		case md.Link:
			t = mt.NewTextEntityTypeTextURL(string(node.Destination))
		case md.Code:
			r.Entities = append(r.Entities, *mt.NewTextEntity(int32(r.Index), int32(l), mt.NewTextEntityTypeCode()))
		case md.CodeBlock:
			var codeBlockType mt.TextEntityType
			if len(node.Info) > 0{
				codeBlockType = mt.NewTextEntityTypePreCode(string(node.Info))
			} else{
				codeBlockType = mt.NewTextEntityTypePre()
			}
			r.Entities = append(r.Entities, *mt.NewTextEntity(int32(r.Index), int32(l), codeBlockType))
		}
		if t != nil{
			ent := mt.NewTextEntity(int32(r.Index), 0, t)
			ent.Extra = extra
			r.openedEntities[node.Type] = mt.NewTextEntity(int32(r.Index), 0, t)
		}
	} else{
		if ent := r.openedEntities[node.Type]; ent != nil{
			ent.Length = int32(r.Index + l) - ent.Offset
			r.Entities = append(r.Entities, *ent)
			r.openedEntities[node.Type] = nil
		}
	}

	r.Index += l
	if _, err := w.Write(node.Literal); err == nil{
		return md.GoToNext
	} else {
		return md.Terminate
	}
}

func (r *TGRenderer) RenderHeader(w io.Writer, node *md.Node){
	_, _ = w.Write(node.Literal)
}

func (r *TGRenderer) RenderFooter(w io.Writer, node *md.Node){
	_, _ = w.Write(node.Literal)
}

func toFormattedText(t string) *mt.FormattedText {
	rnd := NewRenderer()
	data := md.Run([]byte(t), md.WithRenderer(rnd))
	out := mt.NewFormattedText(string(data), rnd.Entities)
	return out
}

func sendMessage(client *mt.Client, chat int64, text string) (int64, error) {
	var err error
	var id int64
	msg := mt.NewInputMessageText(toFormattedText(text), true, true)
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
	//mt.SetLogVerbosityLevel(1)
	mt.SetFilePath("./errors.log")
	client := mt.NewClient(mt.Config{
		APIID:                  "-",
		APIHash:                "-",
		SystemLanguageCode:     "en",
		DeviceModel:            "Server",
		SystemVersion:          "0.0.1",
		ApplicationVersion:     "0.195284w",
		UseTestDataCenter:      false,
		DatabaseDirectory:      "dbdir",
		FileDirectory:          "fdir",
		UseFileDatabase:        false,
		UseChatInfoDatabase:    false,
		UseMessageDatabase:     false,
		UseSecretChats:         false,
		EnableStorageOptimizer: false,
		IgnoreFileNames:        false,
	})
	defer client.DestroyInstance()
	authorize(client)
	go func() {
		for update := range client.GetRawUpdatesChannel(0) {
			fmt.Println(update.Data)
		}
	}()
	//if err := getChatList(client, 1000); err != nil {
	//	println(err.Error())
	//	os.Exit(1)
	//}
	//for _, chat := range allChats {
	//	fmt.Printf("Chat: %d - %s \n", chat.ID, chat.Title)
	//}

	_, _ = sendMessage(client, 1053007259, "**strong-text** \\* _curse-text_\n**_strongcurse-text_** `code-text` ~~stroked-text~~\n[link-text](http://link.addr)\n```go\ncode\nblock\n```")
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
