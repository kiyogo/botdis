package website

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"botdis/internal/discord"
	"botdis/internal/storage"

	"github.com/bwmarrin/discordgo"
)

type Service struct {
	store           *storage.MySQLStore
	sess            *discordgo.Session
	client          *http.Client
	lastStatusCache map[string]string
	cacheLock       sync.RWMutex
	stopChan        chan struct{}
}

func NewService(store *storage.MySQLStore, sess *discordgo.Session) *Service {
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: false,
	}
	s := &Service{
		store:           store,
		sess:            sess,
		client:          &http.Client{Timeout: 10 * time.Second, Transport: tr},
		lastStatusCache: make(map[string]string),
		stopChan:        make(chan struct{}),
	}

	go s.startWorkerLoop()
	return s
}

func (s *Service) Stop() {
	close(s.stopChan)
}

func (s *Service) RegisterRoutes(r *discord.Router) {
	r.RegisterComponent(BtnWebsiteAddModal, s.handleWebsiteAddModalPrompt)
	r.RegisterModal(ModalWebsiteAdd, s.handleWebsiteAddModalSubmit)
	r.RegisterComponent(BtnWebsiteDeleteView, s.handleWebsiteDeleteView)
	r.RegisterComponent(SelectWebsiteDelete, s.handleWebsiteSelectDelete)
	r.RegisterComponent(BtnWebsiteSetupChannel, s.handleWebsiteSetupChannel)
	r.RegisterComponent(BtnWebsiteRefresh, s.handleWebsiteRefresh)
}

func (s *Service) startWorkerLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	time.Sleep(5 * time.Second)
	s.CheckAllWebsites()

	for {
		select {
		case <-ticker.C:
			s.CheckAllWebsites()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Service) CheckAllWebsites() {
	if s.sess == nil {
		return
	}

	configs, err := s.store.GetAllWebsiteStatusConfigs()
	if err != nil || len(configs) == 0 {
		return
	}

	for _, cfg := range configs {
		if cfg.ChannelID == "" {
			continue
		}
		s.checkGuildWebsites(cfg)
	}
}

func (s *Service) checkURL(url string) string {
	targetURL := url
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "http://" + targetURL
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return "offline"
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return "offline"
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 || resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "online"
	}
	return "offline"
}

func (s *Service) checkGuildWebsites(cfg *storage.WebsiteStatusConfig) {
	websites, err := s.store.GetMonitoredWebsites(cfg.GuildID)
	if err != nil || len(websites) == 0 {
		return
	}

	hasChanged := false
	type checkResult struct {
		w      *storage.MonitoredWebsite
		status string
	}

	results := make([]checkResult, len(websites))
	var wg sync.WaitGroup

	for idx, item := range websites {
		wg.Add(1)
		go func(i int, web *storage.MonitoredWebsite) {
			defer wg.Done()
			st := s.checkURL(web.URL)
			results[i] = checkResult{w: web, status: st}
		}(idx, item)
	}
	wg.Wait()

	s.cacheLock.Lock()
	for _, res := range results {
		cacheKey := fmt.Sprintf("%s_%s", cfg.GuildID, res.w.URL)
		lastStatus := s.lastStatusCache[cacheKey]
		if lastStatus != res.status {
			hasChanged = true
			s.lastStatusCache[cacheKey] = res.status
			_ = s.store.UpdateWebsiteStatus(res.w.ID, res.status)
		}
		res.w.Status = res.status
	}
	s.cacheLock.Unlock()

	if hasChanged || cfg.MessageID == "" {
		s.updateDiscordStatusMessage(cfg, websites)
	}
}

func (s *Service) updateDiscordStatusMessage(cfg *storage.WebsiteStatusConfig, websites []*storage.MonitoredWebsite) {
	embed := &discordgo.MessageEmbed{
		Title:     "Trạng thái website",
		Color:     0x3498db,
		Timestamp: time.Now().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Cập nhật mỗi 30 giây",
		},
	}

	var sb strings.Builder
	for _, w := range websites {
		stText := "Hoạt động"
		if w.Status == "offline" {
			stText = "Ngừng hoạt động"
		}
		sb.WriteString(fmt.Sprintf("%s - %s\n", w.URL, stText))
	}

	if sb.Len() == 0 {
		sb.WriteString("Chưa có website nào được theo dõi")
	}
	embed.Description = sb.String()

	if cfg.MessageID != "" {
		_, err := s.sess.ChannelMessageEditEmbed(cfg.ChannelID, cfg.MessageID, embed)
		if err == nil {
			return
		}
	}

	msg, err := s.sess.ChannelMessageSendEmbed(cfg.ChannelID, embed)
	if err == nil && msg != nil {
		cfg.MessageID = msg.ID
		_ = s.store.SaveWebsiteStatusConfig(cfg.GuildID, cfg.ChannelID, msg.ID)
	}
}

// --- Setting GUI Logic ---

func (s *Service) BuildWebsiteAdminView(guildID, channelID string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	websites, _ := s.store.GetMonitoredWebsites(guildID)
	cfg, _ := s.store.GetWebsiteStatusConfig(guildID)

	embed := &discordgo.MessageEmbed{
		Title:       "Website",
		Description: "Quản lý website và kênh",
		Color:       0x7289da,
	}

	channelInfo := "Chưa cài đặt"
	if cfg != nil && cfg.ChannelID != "" {
		channelInfo = fmt.Sprintf("<#%s>", cfg.ChannelID)
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Kênh ghim",
		Value:  channelInfo,
		Inline: false,
	})

	var sb strings.Builder
	if len(websites) == 0 {
		sb.WriteString("Chưa có website")
	} else {
		for _, w := range websites {
			stText := "Hoạt động"
			if w.Status == "offline" {
				stText = "Ngừng hoạt động"
			}
			sb.WriteString(fmt.Sprintf("%s - %s\n", w.URL, stText))
		}
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Danh sách website",
		Value:  sb.String(),
		Inline: false,
	})

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Thêm",
					CustomID: BtnWebsiteAddModal,
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					Label:    "Xóa",
					CustomID: BtnWebsiteDeleteView,
					Style:    discordgo.DangerButton,
				},
				discordgo.Button{
					Label:    "Kênh",
					CustomID: BtnWebsiteSetupChannel,
					Style:    discordgo.PrimaryButton,
				},
				discordgo.Button{
					Label:    "Làm mới",
					CustomID: BtnWebsiteRefresh,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnWebsiteBackToDashboard,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}

	return embed, components
}

func (s *Service) handleWebsiteAddModalPrompt(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalWebsiteAdd,
			Title:    "Thêm website",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "urls_input",
							Label:       "Liên kết",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "google.com",
							Required:    true,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleWebsiteAddModalSubmit(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	rawInput := ""
	for _, row := range data.Components {
		if ar, ok := row.(*discordgo.ActionsRow); ok {
			for _, comp := range ar.Components {
				if ti, ok := comp.(*discordgo.TextInput); ok && ti.CustomID == "urls_input" {
					rawInput = ti.Value
				}
			}
		}
	}

	urlTokens := strings.Fields(rawInput)
	var added []string

	for _, token := range urlTokens {
		urlClean := strings.TrimSpace(token)
		if urlClean == "" {
			continue
		}
		if !strings.HasPrefix(urlClean, "http://") && !strings.HasPrefix(urlClean, "https://") {
			urlClean = "http://" + urlClean
		}

		if _, err := s.store.AddMonitoredWebsite(i.GuildID, urlClean); err == nil {
			added = append(added, urlClean)
		}
	}

	go func() {
		cfg, _ := s.store.GetWebsiteStatusConfig(i.GuildID)
		if cfg != nil && cfg.ChannelID != "" {
			s.checkGuildWebsites(cfg)
		}
	}()

	embed, components := s.BuildWebsiteAdminView(i.GuildID, i.ChannelID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})

	msg := fmt.Sprintf("Đã thêm %d website mới", len(added))
	if len(added) == 0 {
		msg = "Không có website mới nào được thêm"
	}
	_, _ = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: msg,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func (s *Service) handleWebsiteDeleteView(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	websites, _ := s.store.GetMonitoredWebsites(i.GuildID)
	if len(websites) == 0 {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Không có website để xóa",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	var options []discordgo.SelectMenuOption
	for _, w := range websites {
		label := w.URL
		if len(label) > 100 {
			label = label[:100]
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       label,
			Value:       strconv.Itoa(w.ID),
			Description: w.Status,
		})
		if len(options) >= 25 {
			break
		}
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    SelectWebsiteDelete,
					Placeholder: "Chọn website",
					Options:     options,
					MaxValues:   len(options),
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnWebsiteBackToDashboard,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Components: components,
		},
	})
}

func (s *Service) handleWebsiteSelectDelete(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	var ids []int
	for _, val := range data.Values {
		if id, err := strconv.Atoi(val); err == nil {
			ids = append(ids, id)
		}
	}

	deleted, _ := s.store.DeleteMonitoredWebsites(i.GuildID, ids)

	go func() {
		cfg, _ := s.store.GetWebsiteStatusConfig(i.GuildID)
		if cfg != nil && cfg.ChannelID != "" {
			s.checkGuildWebsites(cfg)
		}
	}()

	embed, components := s.BuildWebsiteAdminView(i.GuildID, i.ChannelID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})

	_, _ = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("Đã xóa %d website", len(deleted)),
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func (s *Service) handleWebsiteSetupChannel(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	targetChannelID := i.ChannelID

	_ = s.store.SaveWebsiteStatusConfig(i.GuildID, targetChannelID, "")

	go func() {
		cfg, _ := s.store.GetWebsiteStatusConfig(i.GuildID)
		if cfg != nil {
			s.checkGuildWebsites(cfg)
		}
	}()

	embed, components := s.BuildWebsiteAdminView(i.GuildID, i.ChannelID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})

	_, _ = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("Đã thiết lập kênh <#%s> làm nơi ghim bảng trạng thái", targetChannelID),
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func (s *Service) handleWebsiteRefresh(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Đang làm mới trạng thái website",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})

	cfg, _ := s.store.GetWebsiteStatusConfig(i.GuildID)
	if cfg != nil && cfg.ChannelID != "" {
		go s.checkGuildWebsites(cfg)
	}
}
