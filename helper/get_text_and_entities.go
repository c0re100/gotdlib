package helper

import (
	"encoding/json"
	"strings"
	"unicode/utf16"

	tdlib "github.com/c0re100/gotdlib/client"
)

func richMsg2JSON(data any) []byte {
	if data == nil {
		return []byte("[]")
	}
	respByte, err := json.Marshal(data)
	if err != nil {
		return []byte("[]")
	}
	return respByte
}

type RawRichText struct {
	Type     string          `json:"@type"`
	Text     json.RawMessage `json:"text,omitempty"`
	Texts    []RawRichText   `json:"texts,omitempty"`
	URL      string          `json:"url,omitempty"`
	Language string          `json:"language,omitempty"`
}

type RawCaption struct {
	Type   string       `json:"@type"`
	Text   RawRichText  `json:"text"`
	Credit *RawRichText `json:"credit,omitempty"`
}

type RawPageBlock struct {
	Type     string         `json:"@type"`
	Text     *RawRichText   `json:"text,omitempty"`
	Caption  *RawCaption    `json:"caption,omitempty"`
	Blocks   []RawPageBlock `json:"blocks,omitempty"`
	Language string         `json:"language,omitempty"`
}

type formattedBuilder struct {
	sb       strings.Builder
	utf16Len int32
	entities []*tdlib.TextEntity
}

func (b *formattedBuilder) appendText(str string) {
	b.sb.WriteString(str)
	b.utf16Len += int32(len(utf16.Encode([]rune(str))))
}

func (b *formattedBuilder) walkRichText(rt *RawRichText) {
	if rt == nil {
		return
	}

	startOffset := b.utf16Len

	if rt.Type == "richTexts" && len(rt.Texts) > 0 {
		for i := range rt.Texts {
			b.walkRichText(&rt.Texts[i])
		}
		return
	}

	if len(rt.Text) > 0 {
		var plainStr string
		if err := json.Unmarshal(rt.Text, &plainStr); err == nil {
			b.appendText(plainStr)
		} else {
			var childRaw RawRichText
			if err := json.Unmarshal(rt.Text, &childRaw); err == nil {
				b.walkRichText(&childRaw)
			}
		}
	}

	length := b.utf16Len - startOffset
	if length == 0 {
		return
	}

	var entityType tdlib.TextEntityType
	switch rt.Type {
	case "richTextBold":
		entityType = &tdlib.TextEntityTypeBold{}
	case "richTextItalic":
		entityType = &tdlib.TextEntityTypeItalic{}
	case "richTextUnderline":
		entityType = &tdlib.TextEntityTypeUnderline{}
	case "richTextStrikethrough":
		entityType = &tdlib.TextEntityTypeStrikethrough{}
	case "richTextSpoiler":
		entityType = &tdlib.TextEntityTypeSpoiler{}
	case "richTextCode":
		entityType = &tdlib.TextEntityTypeCode{}
	case "richTextUrl":
		entityType = &tdlib.TextEntityTypeTextUrl{
			Url: rt.URL,
		}
	case "richTextEmailAddress":
		entityType = &tdlib.TextEntityTypeEmailAddress{}
	case "richTextFixed", "richTextPre":
		entityType = &tdlib.TextEntityTypePreCode{
			Language: rt.Language,
		}
	}

	if entityType != nil {
		b.entities = append(b.entities, &tdlib.TextEntity{
			Offset: startOffset,
			Length: length,
			Type:   entityType,
		})
	}
}

func (b *formattedBuilder) walkBlock(block *RawPageBlock) {
	if block == nil {
		return
	}

	startOffset := b.utf16Len

	if block.Text != nil {
		b.walkRichText(block.Text)
	}

	if block.Caption != nil {
		b.walkRichText(&block.Caption.Text)
		if block.Caption.Credit != nil {
			b.walkRichText(block.Caption.Credit)
		}
	}

	length := b.utf16Len - startOffset
	if length > 0 {
		switch block.Type {
		case "pageBlockPreformatted":
			b.entities = append(b.entities, &tdlib.TextEntity{
				Offset: startOffset,
				Length: length,
				Type: &tdlib.TextEntityTypePreCode{
					Language: block.Language,
				},
			})
		case "pageBlockBlockQuote", "pageBlockPullQuote":
			b.entities = append(b.entities, &tdlib.TextEntity{
				Offset: startOffset,
				Length: length,
				Type:   &tdlib.TextEntityTypeBlockQuote{},
			})
		}
	}

	for i := range block.Blocks {
		b.walkBlock(&block.Blocks[i])
	}
}

func GetTextAndEntitiesFromRichMessage(msg *tdlib.RichMessage) (*tdlib.FormattedText, error) {
	jsonBytes := richMsg2JSON(msg.Blocks)

	var blocks []RawPageBlock
	if err := json.Unmarshal(jsonBytes, &blocks); err != nil {
		return nil, err
	}

	builder := &formattedBuilder{}

	for i := range blocks {
		prevLen := builder.sb.Len()

		builder.walkBlock(&blocks[i])

		if builder.sb.Len() > prevLen && i < len(blocks)-1 {
			builder.appendText("\n")
		}
	}

	if builder.entities == nil {
		builder.entities = []*tdlib.TextEntity{}
	}

	return &tdlib.FormattedText{
		Text:     builder.sb.String(),
		Entities: builder.entities,
	}, nil
}

func GetTextAndEntities(msg *tdlib.Message) (string, []*tdlib.TextEntity, string) {
	var msgText string
	var msgEnt []*tdlib.TextEntity
	msgType := strings.ReplaceAll(msg.Content.MessageContentType(), "message", "")

	switch msg.Content.MessageContentType() {
	case tdlib.TypeMessageText:
		msgText = msg.Content.(*tdlib.MessageText).Text.Text
		msgEnt = msg.Content.(*tdlib.MessageText).Text.Entities
	case tdlib.TypeMessageRichMessage:
		richMsg := msg.Content.(*tdlib.MessageRichMessage).Message
		formatted, err := GetTextAndEntitiesFromRichMessage(richMsg)
		if err != nil {
			return "", []*tdlib.TextEntity{}, msgType
		}
		msgText = formatted.Text
		msgEnt = formatted.Entities
	case tdlib.TypeMessageAnimation:
		msgText = msg.Content.(*tdlib.MessageAnimation).Caption.Text
		msgEnt = msg.Content.(*tdlib.MessageAnimation).Caption.Entities
	case tdlib.TypeMessageAudio:
		msgText = msg.Content.(*tdlib.MessageAudio).Caption.Text
		msgEnt = msg.Content.(*tdlib.MessageAudio).Caption.Entities
	case tdlib.TypeMessageDocument:
		msgText = msg.Content.(*tdlib.MessageDocument).Caption.Text
		msgEnt = msg.Content.(*tdlib.MessageDocument).Caption.Entities
	case tdlib.TypeMessagePhoto:
		msgText = msg.Content.(*tdlib.MessagePhoto).Caption.Text
		msgEnt = msg.Content.(*tdlib.MessagePhoto).Caption.Entities
	case tdlib.TypeMessagePaidMedia:
		msgText = msg.Content.(*tdlib.MessagePaidMedia).Caption.Text
		msgEnt = msg.Content.(*tdlib.MessagePaidMedia).Caption.Entities
	case tdlib.TypeMessageVideo:
		msgText = msg.Content.(*tdlib.MessageVideo).Caption.Text
		msgEnt = msg.Content.(*tdlib.MessageVideo).Caption.Entities
	case tdlib.TypeMessageVoiceNote:
		msgText = msg.Content.(*tdlib.MessageVoiceNote).Caption.Text
		msgEnt = msg.Content.(*tdlib.MessageVoiceNote).Caption.Entities
	case tdlib.TypeMessageContact:
		msgText = msg.Content.(*tdlib.MessageContact).Contact.FirstName + msg.Content.(*tdlib.MessageContact).Contact.LastName
		msgEnt = []*tdlib.TextEntity{}
	default:
		return "", []*tdlib.TextEntity{}, msgType
	}

	return msgText, msgEnt, msgType
}
