package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "embed"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/state"
	"github.com/diamondburned/gotk4/pkg/core/gextras"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gdkpixbuf/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/diamondburned/ningen/v2"
	"github.com/gotk3/gotk3/glib"
)

//go:embed style.css
var styleCSS string

type app struct {
	app   *gtk.Application
	Main  *gtk.ApplicationWindow
	State *ningen.State

	Guilds      *guildView
	GuildScroll *gtk.ScrolledWindow

	Channels      *channelView
	ChannelScroll *gtk.ScrolledWindow

	Messages      *messageView
	MessageScroll *gtk.ScrolledWindow
}

type guildView struct {
	list  *gtk.ListBox
	store *guildStore
}

type guildStore struct {
	guilds []guild
}

func newGuildView(onGuild func(*guild)) *guildView {
	list := gtk.NewListBox()
	list.SetActivateOnSingleClick(true)
	list.Show()

	var store guildStore

	list.InitiallyUnowned.Connect("row-activated", func(list *gtk.ListBox, row *gtk.ListBoxRow) {
		onGuild(&store.guilds[row.Index()])
	})

	return &guildView{
		list:  list,
		store: &store,
	}
}

type guild struct {
	ID   discord.GuildID
	Name string // tooltip
	Icon *gtk.Image
}

func (view *guildView) addGuild(g *discord.Guild) {
	icon := gtk.NewImageFromIconName("system-user-symbolic")
	icon.SetSizeRequest(48, 48)
	// icon.SetCSSClasses([]string{"guild-icon"})
	icon.StyleContext().AddClass("guild-icon")
	icon.Show()

	if g.Icon != "" {
		asyncSetImage(icon, g.IconURLWithType(discord.PNGImage)+"?size=64")
	}

	row := gtk.NewListBoxRow()
	row.StyleContext().AddClass("guild-row")
	row.SetChild(icon)
	row.SetTooltipText(g.Name)
	row.Show()

	view.store.guilds = append(view.store.guilds, guild{g.ID, g.Name, icon})
	view.list.Append(row)
}

type channelView struct {
	list  *gtk.ListBox // TODO embed
	store *channelStore
}

type channelStore struct {
	channels []channel
}

func newChannelView(onChannel func(*channel)) *channelView {
	list := gtk.NewListBox()
	list.SetActivateOnSingleClick(true)
	list.Show()

	var store channelStore

	list.InitiallyUnowned.Connect("row-activated", func(list *gtk.ListBox, row *gtk.ListBoxRow) {
		onChannel(&store.channels[row.Index()])
	})

	return &channelView{
		list:  list,
		store: &store,
	}
}

type channel struct {
	ID    discord.ChannelID
	Guild discord.GuildID
	Name  *gtk.Label
}

func (view *channelView) addChannel(ch *discord.Channel) {
	if ch.Type != discord.GuildText {
		return
	}

	name := gtk.NewLabel("#" + ch.Name)
	name.SetXalign(0)
	name.SetWrapMode(pango.WrapModeWordChar)
	name.Show()

	channel := channel{ch.ID, ch.GuildID, name}
	view.store.channels = append(view.store.channels, channel)

	row := gtk.NewListBoxRow()
	row.StyleContext().AddClass("channel-row")
	row.SetChild(name)
	row.Show()

	view.list.Append(row)
}

type messageView struct {
	list  *gtk.ListBox
	store *messageStore
}

type messageStore struct {
	messages []message
}

func newMessageView() *messageView {
	list := gtk.NewListBox()
	list.SetSelectionMode(gtk.SelectionModeNone)
	list.Show()

	return &messageView{
		list:  list,
		store: &messageStore{},
	}
}

type message struct {
	ID       discord.MessageID
	AuthorID discord.UserID
	Avatar   *gtk.Image
	Author   *gtk.Label
	Content  *gtk.TextView
}

func (view *messageView) canShrink(msg *discord.Message) bool {
	if len(view.store.messages) == 0 {
		return false
	}

	last := view.store.messages[len(view.store.messages)-1]
	return last.AuthorID == msg.Author.ID
}

func (view *messageView) addMessage(msg *discord.Message) {
	if view.canShrink(msg) {
		content := gtk.NewTextView()
		content.SetWrapMode(gtk.WrapModeWordChar)
		// content.SetCSSClasses([]string{"message-content"})
		content.StyleContext().AddClass("message-content")
		content.Show()

		buffer := content.Buffer()
		buffer.SetText(msg.Content, len(msg.Content))

		row := gtk.NewListBoxRow()
		row.StyleContext().AddClass("message-row")
		row.StyleContext().AddClass("message-compact")
		row.SetChild(content)
		row.Show()

		message := message{msg.ID, msg.Author.ID, nil, nil, content}
		view.store.messages = append(view.store.messages, message)

		view.list.Append(row)
		return
	}

	avatar := gtk.NewImage()
	avatar.SetVAlign(gtk.AlignStart)
	avatar.SetSizeRequest(32, 32)
	// avatar.SetCSSClasses([]string{"avatar"})
	avatar.StyleContext().AddClass("avatar")
	avatar.Show()

	if msg.Author.Avatar != "" {
		asyncSetImage(avatar, msg.Author.AvatarURLWithType(discord.PNGImage)+"?size=64")
	}

	author := gtk.NewLabel("<b>" + html.EscapeString(msg.Author.Username) + "</b>")
	author.SetUseMarkup(true)
	author.SetXalign(0)
	author.SetWrapMode(pango.WrapModeWordChar)
	// author.SetCSSClasses([]string{"username"})
	author.StyleContext().AddClass("username")
	author.Show()

	content := gtk.NewTextView()
	content.SetWrapMode(gtk.WrapModeWordChar)
	// content.SetCSSClasses([]string{"message-content"})
	content.StyleContext().AddClass("message-content")
	content.Show()

	buffer := content.Buffer()
	buffer.SetText(msg.Content, len(msg.Content))

	message := message{msg.ID, msg.Author.ID, avatar, author, content}
	view.store.messages = append(view.store.messages, message)

	rightBox := gtk.NewBox(gtk.OrientationVertical, 0)
	rightBox.SetHExpand(true)
	rightBox.Append(author)
	rightBox.Append(content)
	rightBox.Show()

	box := gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.Append(avatar)
	box.Append(rightBox)
	box.Show()

	row := gtk.NewListBoxRow()
	row.StyleContext().AddClass("message-row")
	row.SetChild(box)
	row.Show()

	view.list.Append(row)
}

func asyncSetImage(image *gtk.Image, url string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	image.Widget.InitiallyUnowned.Connect("destroy", cancel)

	go func() {
		defer cancel()

		r, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			log.Println("URL error:", err)
			return
		}

		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			log.Println("image Do error:", err)
			return
		}
		defer resp.Body.Close()

		loader, err := gdkpixbuf.NewPixbufLoaderWithType("png")
		if err != nil {
			log.Println("unknown PNG format:", err)
			return
		}
		if err := pixbufLoaderReadFrom(loader, resp.Body); err != nil {
			log.Println("image fetch error:", err)
			return
		}

		pixbuf := loader.Pixbuf()
		glib.IdleAdd(func() {
			image.SetFromPixbuf(pixbuf)
		})
	}()
}

type pixbufLoaderWriter gdkpixbuf.PixbufLoader

func pixbufLoaderReadFrom(l *gdkpixbuf.PixbufLoader, r io.Reader) error {
	_, err := io.Copy((*pixbufLoaderWriter)(l), r)
	if err != nil {
		return err
	}
	if err := l.Close(); err != nil {
		return fmt.Errorf("failed to close PixbufLoader: %w", err)
	}
	return nil
}

func (w *pixbufLoaderWriter) Write(b []byte) (int, error) {
	if err := (*gdkpixbuf.PixbufLoader)(w).Write(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

var token = os.Getenv("TOKEN")

func init() {
	if token == "" {
		log.Fatalln("missing $TOKEN")
	}
}

func main() {
	app := gtk.NewApplication("com.github.diamondburned.toycord4", 0)
	app.Connect("activate", start)

	if exit := app.Run(os.Args); exit > 0 {
		os.Exit(exit)
	}
}

func start(gApp *gtk.Application) {
	spinner := gtk.NewSpinner()
	spinner.SetHAlign(gtk.AlignCenter)
	spinner.SetVAlign(gtk.AlignCenter)
	spinner.SetSizeRequest(32, 32)
	spinner.Start()
	spinner.Show()

	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.SetHExpand(true)
	box.SetVExpand(true)
	box.Append(spinner)
	box.Show()

	w := gtk.NewApplicationWindow(gApp)
	w.SetTitle("toycord4")
	w.SetDefaultSize(600, 450)
	w.SetChild(box)
	w.Show()

	css := gtk.NewCSSProvider()
	css.LoadFromData([]byte(styleCSS))

	display := gextras.MustGet(&w.InitiallyUnowned, "display").(*gdk.Display)
	gtk.StyleContextAddProviderForDisplay(display, css, 600)

	go func() {
		s, err := state.New(token)
		if err != nil {
			log.Println("failed to start state:", err)
			return
		}

		n, err := ningen.FromState(s)
		if err != nil {
			log.Println("failed to wrap state:", err)
			return
		}

		if err := s.Open(); err != nil {
			log.Println("failed to connect:", err)
			return
		}

		glib.IdleAdd(func() {
			bindDiscord(&app{
				app:   gApp,
				Main:  w,
				State: n,
			})
		})
	}()
}

func bindDiscord(app *app) {
	app.GuildScroll = gtk.NewScrolledWindow()
	app.GuildScroll.SetPolicy(gtk.PolicyTypeNever, gtk.PolicyTypeAutomatic)
	app.GuildScroll.Show()

	app.ChannelScroll = gtk.NewScrolledWindow()
	app.ChannelScroll.SetSizeRequest(200, -1)
	app.ChannelScroll.SetPolicy(gtk.PolicyTypeNever, gtk.PolicyTypeAutomatic)
	app.ChannelScroll.Show()

	app.MessageScroll = gtk.NewScrolledWindow()
	app.MessageScroll.SetHExpand(true)
	app.MessageScroll.SetPolicy(gtk.PolicyTypeNever, gtk.PolicyTypeAutomatic)
	app.MessageScroll.Show()

	viewBox := gtk.NewBox(gtk.OrientationHorizontal, 0)
	viewBox.Append(app.GuildScroll)
	viewBox.Append(app.ChannelScroll)
	viewBox.Append(app.MessageScroll)
	viewBox.Show()

	guilds, err := app.State.Guilds()
	if err != nil {
		log.Println("failed to get guilds:", err)
		return
	}

	app.Guilds = newGuildView(app.selectGuild)
	app.Guilds.list.Show()
	app.GuildScroll.SetChild(app.Guilds.list)

	for i := range guilds {
		app.Guilds.addGuild(&guilds[i])
	}

	app.Main.SetChild(viewBox)
}

func (app *app) selectGuild(g *guild) {
	loading := gtk.NewSpinner()
	loading.Start()
	loading.Show()

	app.ChannelScroll.SetChild(loading)
	app.Channels = nil

	go func() {
		channels, err := app.State.Channels(g.ID)
		if err != nil {
			log.Println("failed to get channels:", err)
			return
		}

		glib.IdleAdd(func() {
			app.loadChannels(channels)
		})
	}()
}

func (app *app) loadChannels(channels []discord.Channel) {
	app.Channels = newChannelView(app.selectChannel)
	app.Channels.list.Show()
	app.ChannelScroll.SetChild(app.Channels.list)

	for i := range channels {
		app.Channels.addChannel(&channels[i])
	}
}

func (app *app) selectChannel(ch *channel) {
	loading := gtk.NewSpinner()
	loading.Start()
	loading.Show()

	app.MessageScroll.SetChild(loading)
	app.Messages = nil

	go func() {
		messages, err := app.State.Messages(ch.ID)
		if err != nil {
			log.Println("failed to get messages:", err)
			return
		}

		glib.IdleAdd(func() {
			app.loadMessages(messages)
		})

		if ch.Guild.IsValid() {
			app.State.MemberState.Subscribe(ch.Guild)
		}
	}()
}

func (app *app) loadMessages(messages []discord.Message) {
	app.Messages = newMessageView()
	app.Messages.list.Show()
	app.MessageScroll.SetChild(app.Messages.list)

	for i := range messages {
		app.Messages.addMessage(&messages[i])
	}
}
