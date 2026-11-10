package dashboard

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"botdis/internal/discord"
	"botdis/internal/modules/feed"
	"botdis/internal/modules/profile"
	"botdis/internal/modules/sticky"
	"botdis/internal/modules/ticket"
	"botdis/internal/modules/utility"
	"botdis/internal/modules/website"
	"botdis/internal/storage"

	"github.com/bwmarrin/discordgo"
)

const (
	BtnViewTicket               = "setting_view_ticket"
	SelectTicketRole            = "setting_select_ticket_role"
	SelectTicketCategory        = "setting_select_ticket_category"
	SelectTicketArchiveCategory = "setting_select_ticket_archive_category"
	BtnTicketSendPanel          = "setting_ticket_send_panel"

	BtnViewAutoRole    = "setting_view_autorole"
	SelectAutoRole     = "setting_select_autorole"
	BtnAutoRoleDelete  = "setting_autorole_delete"
	BtnAutoRoleDelay   = "setting_autorole_delay"
	ModalAutoRoleDelay = "modal_autorole_delay"

	BtnViewFeed         = "setting_view_feed"
	BtnAddFeedYoutube   = "setting_add_feed_youtube"
	BtnAddFeedTiktok    = "setting_add_feed_tiktok"
	SelectFeedRole      = "setting_select_feed_role"
	BtnFeedNoPing       = "setting_feed_no_ping"
	SelectFeedChannel   = "setting_select_feed_channel"
	SelectDeleteFeed    = "setting_select_delete_feed"
	ModalAddFeedYoutube = "modal_add_feed_youtube"
	ModalAddFeedTiktok  = "modal_add_feed_tiktok"

	BtnSettingSticky    = "setting_sticky"
	ModalSettingSticky  = "modal_setting_sticky"
	InputStickyContent  = "input_sticky_content"
	BtnViewRoleMenu     = "setting_view_role_menu"
	SelectRoleMenuRoles = "setting_select_role_menu_roles"
	RoleMenuAssign      = "role_menu_assign"

	BtnViewBotInfo        = "setting_view_botinfo"
	BtnViewPing           = "setting_view_ping"
	BtnViewProfileAdmin   = "setting_view_profile_admin"
	BtnViewWebsiteAdmin   = "setting_view_website_admin"
	BtnActionSyncCommands = "setting_action_sync_commands"
	BtnBackToMain         = "setting_back"
)

type feedSessionState struct {
	targetChannelID string
	pingRoleID      string
}

var userFeedState sync.Map

type Service struct {
	store          *storage.MySQLStore
	stickyService  *sticky.Service
	profileService *profile.Service
	websiteService *website.Service
	commands       []*discordgo.ApplicationCommand
}

func NewService(store *storage.MySQLStore, stickyService *sticky.Service, profileService *profile.Service, websiteService *website.Service) *Service {
	return &Service{
		store:          store,
		stickyService:  stickyService,
		profileService: profileService,
		websiteService: websiteService,
	}
}

func (s *Service) SetCommands(cmds []*discordgo.ApplicationCommand) {
	s.commands = cmds
}

func (s *Service) RegisterRoutes(r *discord.Router) {
	r.RegisterCommand("setting", s.handleSettingCommand)

	toggleKeys := []string{
		"ac_link", "ac_media", "ac_file", "ac_text",
		"as_fast", "as_dup",
		"level_enabled", "tiktok_enabled", "ai_enabled",
	}

	for _, key := range toggleKeys {
		customID := "toggle_" + key
		k := key
		r.RegisterComponent(customID, func(sess *discordgo.Session, i *discordgo.InteractionCreate) {
			s.handleToggle(sess, i, k)
		})
	}

	r.RegisterComponent(BtnViewTicket, s.handleViewTicket)
	r.RegisterComponent(SelectTicketRole, s.handleSelectTicketRole)
	r.RegisterComponent(SelectTicketCategory, s.handleSelectTicketCategory)
	r.RegisterComponent(SelectTicketArchiveCategory, s.handleSelectTicketArchiveCategory)
	r.RegisterComponent(BtnTicketSendPanel, s.handleTicketSendPanel)

	r.RegisterComponent(BtnViewAutoRole, s.handleViewAutoRole)
	r.RegisterComponent(SelectAutoRole, s.handleSelectAutoRole)
	r.RegisterComponent(BtnAutoRoleDelete, s.handleAutoRoleDelete)
	r.RegisterComponent(BtnAutoRoleDelay, s.handleAutoRoleDelayPrompt)
	r.RegisterModal(ModalAutoRoleDelay, s.handleAutoRoleDelaySubmit)

	r.RegisterComponent(BtnViewFeed, s.handleViewFeed)
	r.RegisterComponent(BtnAddFeedYoutube, s.handleAddFeedYoutubePrompt)
	r.RegisterComponent(BtnAddFeedTiktok, s.handleAddFeedTiktokPrompt)
	r.RegisterComponent(SelectFeedRole, s.handleSelectFeedRole)
	r.RegisterComponent(BtnFeedNoPing, s.handleFeedNoPing)
	r.RegisterComponent(SelectFeedChannel, s.handleSelectFeedChannel)
	r.RegisterComponent(SelectDeleteFeed, s.handleSelectDeleteFeed)
	r.RegisterModal(ModalAddFeedYoutube, s.handleAddFeedYoutubeSubmit)
	r.RegisterModal(ModalAddFeedTiktok, s.handleAddFeedTiktokSubmit)

	r.RegisterComponent(BtnSettingSticky, s.handleSettingSticky)
	r.RegisterModal(ModalSettingSticky, s.handleSubmitSettingSticky)

	r.RegisterComponent(BtnViewRoleMenu, s.handleViewRoleMenu)
	r.RegisterComponent(SelectRoleMenuRoles, s.handleSelectRoleMenuRoles)
	r.RegisterComponent(RoleMenuAssign, s.handleRoleMenuAssign)

	r.RegisterComponent(BtnViewBotInfo, s.handleViewBotInfo)
	r.RegisterComponent(BtnViewPing, s.handleViewPing)
	r.RegisterComponent(BtnViewProfileAdmin, s.handleViewProfileAdmin)
	r.RegisterComponent(BtnViewWebsiteAdmin, s.handleViewWebsiteAdmin)
	r.RegisterComponent(BtnActionSyncCommands, s.handleActionSyncCommands)
	r.RegisterComponent(BtnBackToMain, s.handleBackToMain)

	r.RegisterComponent(profile.BtnProfileAdminBackToDashboard, s.handleBackToMain)
	r.RegisterComponent(website.BtnWebsiteBackToDashboard, s.handleBackToMain)

	r.RegisterCommand("sync", s.handleSyncCommand)
}

func (s *Service) handleSettingCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	if !s.isUserAdmin(sess, i.GuildID, i.Member.User.ID) {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Bạn cần quyền Administrator để mở bảng cài đặt",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	embed := s.buildDashboardEmbed(i.GuildID, i.ChannelID)
	components := s.buildDashboardComponents(i.GuildID, i.ChannelID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	})
}

func (s *Service) handleToggle(sess *discordgo.Session, i *discordgo.InteractionCreate, key string) {
	guildID := i.GuildID
	channelID := i.ChannelID

	if key == "as_fast" || key == "as_dup" {
		gData := s.store.GetGuildData(guildID)
		newFast := gData.ASFast
		newDup := gData.ASDup
		if key == "as_fast" {
			newFast = !newFast
		} else {
			newDup = !newDup
		}
		s.store.SetGuildAntiSpam(guildID, newFast, newDup)
	} else {
		s.store.UpdateChannelSetting(guildID, channelID, func(cs *storage.ChannelSettings) {
			switch key {
			case "ac_link":
				cs.ACLink = !cs.ACLink
			case "ac_media":
				cs.ACMedia = !cs.ACMedia
			case "ac_file":
				cs.ACFile = !cs.ACFile
			case "ac_text":
				cs.ACText = !cs.ACText
			case "level_enabled":
				cs.LevelEnabled = !cs.LevelEnabled
			case "tiktok_enabled":
				cs.TikTokEnabled = !cs.TikTokEnabled
			case "ai_enabled":
				cs.AIEnabled = !cs.AIEnabled
			}
		})
	}

	embed := s.buildDashboardEmbed(guildID, channelID)
	components := s.buildDashboardComponents(guildID, channelID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleViewTicket(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	embed, components := s.buildTicketConfigView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleSelectTicketRole(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) > 0 {
		roleID := data.Values[0]
		_ = s.store.SetTicketStaffRole(i.GuildID, roleID)
	}

	embed, components := s.buildTicketConfigView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleSelectTicketCategory(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) > 0 {
		catID := data.Values[0]
		_ = s.store.SetTicketCategory(i.GuildID, catID)
	}

	embed, components := s.buildTicketConfigView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleSelectTicketArchiveCategory(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) > 0 {
		catID := data.Values[0]
		_ = s.store.SetTicketArchiveCategory(i.GuildID, catID)
	}

	embed, components := s.buildTicketConfigView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleTicketSendPanel(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := ticket.BuildSupportPanelEmbed()
	components := ticket.BuildSupportPanelActionRow()

	_, err := sess.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})

	msg := "Đã gửi bảng hỗ trợ vào kênh này"
	if err != nil {
		msg = "Không thể gửi bảng hỗ trợ"
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (s *Service) buildTicketConfigView(guildID string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	tCfg := s.store.GetTicketConfig(guildID)

	staffText := "Chưa thiết lập"
	if tCfg.StaffRoleID != "" {
		staffText = fmt.Sprintf("<@&%s>", tCfg.StaffRoleID)
	}

	catText := "Mặc định"
	if tCfg.TicketCategoryID != "" {
		catText = fmt.Sprintf("<#%s>", tCfg.TicketCategoryID)
	}

	archiveCatText := "Chưa thiết lập"
	if tCfg.ArchiveCategoryID != "" {
		archiveCatText = fmt.Sprintf("<#%s>", tCfg.ArchiveCategoryID)
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Cấu hình ticket",
		Description: fmt.Sprintf("Role hỗ trợ hiện tại **%s**\nDanh mục hoạt động **%s**\nDanh mục lưu trữ **%s**\nChọn role hỗ trợ và danh mục bên dưới", staffText, catText, archiveCatText),
		Color:       0x3498db,
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.RoleSelectMenu,
					CustomID:    SelectTicketRole,
					Placeholder: "Chọn role hỗ trợ...",
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:     discordgo.ChannelSelectMenu,
					CustomID:     SelectTicketCategory,
					Placeholder:  "Chọn danh mục ticket...",
					ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildCategory},
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:     discordgo.ChannelSelectMenu,
					CustomID:     SelectTicketArchiveCategory,
					Placeholder:  "Chọn danh mục lưu trữ...",
					ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildCategory},
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Gửi bảng",
					CustomID: BtnTicketSendPanel,
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnBackToMain,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}

	return embed, components
}

func (s *Service) handleViewAutoRole(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	embed, components := s.buildAutoRoleView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleSelectAutoRole(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) > 0 {
		roleID := data.Values[0]
		s.store.SetAutoRole(i.GuildID, roleID)
	}

	embed, components := s.buildAutoRoleView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAutoRoleDelete(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.store.ClearAutoRole(i.GuildID)
	embed, components := s.buildAutoRoleView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAutoRoleDelayPrompt(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	gData := s.store.GetGuildData(i.GuildID)
	val := fmt.Sprintf("%d", gData.AutoJoinDelay)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalAutoRoleDelay,
			Title:    "Thời gian cấp role",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "delay_seconds",
							Label:       "Thời gian chờ",
							Placeholder: "Số giây",
							Value:       val,
							Style:       discordgo.TextInputShort,
							Required:    true,
							MaxLength:   6,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleAutoRoleDelaySubmit(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	secVal := 0
	if len(data.Components) > 0 {
		if row, ok := data.Components[0].(*discordgo.ActionsRow); ok && len(row.Components) > 0 {
			if input, ok := row.Components[0].(*discordgo.TextInput); ok {
				var parsed int
				if _, err := fmt.Sscanf(input.Value, "%d", &parsed); err == nil && parsed >= 0 {
					secVal = parsed
				}
			}
		}
	}

	_ = s.store.SetAutoRoleDelay(i.GuildID, secVal)
	embed, components := s.buildAutoRoleView(i.GuildID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) buildAutoRoleView(guildID string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	gData := s.store.GetGuildData(guildID)
	roleID := gData.AutoJoinRoles["default"]
	currentText := "Chưa thiết lập"
	if roleID != "" {
		currentText = fmt.Sprintf("<@&%s>", roleID)
	}

	delayText := "Ngay lập tức"
	if gData.AutoJoinDelay > 0 {
		delayText = fmt.Sprintf("%d giây", gData.AutoJoinDelay)
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Cấu hình autorole",
		Description: fmt.Sprintf("Role tự động hiện tại **%s**\nThời gian cấp **%s**\nChọn role bên dưới để tự động gán cho thành viên mới", currentText, delayText),
		Color:       0x3498db,
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.RoleSelectMenu,
					CustomID:    SelectAutoRole,
					Placeholder: "Chọn role tự động...",
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Xóa",
					CustomID: BtnAutoRoleDelete,
					Style:    discordgo.DangerButton,
				},
				discordgo.Button{
					Label:    "Cài đặt",
					CustomID: BtnAutoRoleDelay,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnBackToMain,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}
	return embed, components
}

func (s *Service) handleViewBotInfo(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := utility.BuildBotInfoEmbed()
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnBackToMain,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleViewPing(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	latency := sess.HeartbeatLatency().Milliseconds()
	embed := &discordgo.MessageEmbed{
		Title:       "Độ trễ gateway",
		Description: fmt.Sprintf("Độ trễ gateway hiện tại **%dms**", latency),
		Color:       0x2ecc71,
	}
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Kiểm tra lại",
					CustomID: BtnViewPing,
					Style:    discordgo.PrimaryButton,
				},
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnBackToMain,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleBackToMain(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := s.buildDashboardEmbed(i.GuildID, i.ChannelID)
	components := s.buildDashboardComponents(i.GuildID, i.ChannelID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) buildDashboardEmbed(guildID, channelID string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "Cài đặt",
		Description: "Chọn tính năng để quản lý",
		Color:       0x3498db,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "Chặn spam",
				Value: "**Chặn link**\n" +
					"**Chặn ảnh**\n" +
					"**Chặn tệp**\n" +
					"**Chặn chữ**\n" +
					"**Gửi nhanh**\n" +
					"**Gửi trùng**",
			},
			{
				Name: "Tính năng",
				Value: "**Cấp độ**\n" +
					"**Tiktok**\n" +
					"**Trò chuyện**\n" +
					"**Hỗ trợ**\n" +
					"**Tự cấp vai trò**\n" +
					"**Video**\n" +
					"**Ghim**\n" +
					"**Bảng vai trò**\n" +
					"**Hồ sơ**\n" +
					"**Website**\n" +
					"**Đồng bộ**",
			},
		},
	}
}

func (s *Service) buildDashboardComponents(guildID, channelID string) []discordgo.MessageComponent {
	settings := s.store.GetChannelSettings(guildID, channelID)

	btnStyle := func(enabled bool) discordgo.ButtonStyle {
		if enabled {
			return discordgo.SuccessButton
		}
		return discordgo.DangerButton
	}

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Cấm link", CustomID: "toggle_ac_link", Style: btnStyle(settings.ACLink)},
				discordgo.Button{Label: "Cấm ảnh", CustomID: "toggle_ac_media", Style: btnStyle(settings.ACMedia)},
				discordgo.Button{Label: "Cấm tệp", CustomID: "toggle_ac_file", Style: btnStyle(settings.ACFile)},
				discordgo.Button{Label: "Cấm chữ", CustomID: "toggle_ac_text", Style: btnStyle(settings.ACText)},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Gửi nhanh", CustomID: "toggle_as_fast", Style: btnStyle(settings.ASFast)},
				discordgo.Button{Label: "Gửi trùng", CustomID: "toggle_as_dup", Style: btnStyle(settings.ASDup)},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Cấp độ", CustomID: "toggle_level_enabled", Style: btnStyle(settings.LevelEnabled)},
				discordgo.Button{Label: "Tiktok", CustomID: "toggle_tiktok_enabled", Style: btnStyle(settings.TikTokEnabled)},
				discordgo.Button{Label: "Trò chuyện", CustomID: "toggle_ai_enabled", Style: btnStyle(settings.AIEnabled)},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Hỗ trợ",
					CustomID: BtnViewTicket,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Tự cấp vai trò",
					CustomID: BtnViewAutoRole,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Video",
					CustomID: BtnViewFeed,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Ghim",
					CustomID: BtnSettingSticky,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Bảng vai trò",
					CustomID: BtnViewRoleMenu,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Thông tin",
					CustomID: BtnViewBotInfo,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Độ trễ",
					CustomID: BtnViewPing,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Hồ sơ",
					CustomID: BtnViewProfileAdmin,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Website",
					CustomID: BtnViewWebsiteAdmin,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Đồng bộ",
					CustomID: BtnActionSyncCommands,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}
}

func (s *Service) isUserAdmin(sess *discordgo.Session, guildID, userID string) bool {
	member, err := sess.GuildMember(guildID, userID)
	if err != nil || member == nil {
		return false
	}
	guild, err := sess.Guild(guildID)
	if err != nil || guild == nil {
		return false
	}
	if guild.OwnerID == userID {
		return true
	}
	for _, roleID := range member.Roles {
		for _, r := range guild.Roles {
			if r.ID == roleID && (r.Permissions&discordgo.PermissionAdministrator != 0) {
				return true
			}
		}
	}
	return false
}

func getFeedSessionState(guildID, userID, defaultChannelID string) *feedSessionState {
	key := guildID + "_" + userID
	val, ok := userFeedState.Load(key)
	if !ok {
		state := &feedSessionState{
			targetChannelID: defaultChannelID,
			pingRoleID:      "",
		}
		userFeedState.Store(key, state)
		return state
	}
	return val.(*feedSessionState)
}

func (s *Service) handleViewFeed(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	embed, components := s.buildFeedConfigView(i.GuildID, i.ChannelID, i.Member.User.ID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleSelectFeedRole(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) > 0 {
		state := getFeedSessionState(i.GuildID, i.Member.User.ID, i.ChannelID)
		state.pingRoleID = data.Values[0]
	}

	embed, components := s.buildFeedConfigView(i.GuildID, i.ChannelID, i.Member.User.ID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleFeedNoPing(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	state := getFeedSessionState(i.GuildID, i.Member.User.ID, i.ChannelID)
	state.pingRoleID = ""

	embed, components := s.buildFeedConfigView(i.GuildID, i.ChannelID, i.Member.User.ID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleSelectFeedChannel(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) > 0 {
		state := getFeedSessionState(i.GuildID, i.Member.User.ID, i.ChannelID)
		state.targetChannelID = data.Values[0]
	}

	embed, components := s.buildFeedConfigView(i.GuildID, i.ChannelID, i.Member.User.ID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAddFeedYoutubePrompt(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalAddFeedYoutube,
			Title:    "Thêm kênh YouTube",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "channel_url",
							Label:       "Đường dẫn kênh YouTube",
							Placeholder: "https://www.youtube.com/@tên_kênh hoặc ID kênh",
							Style:       discordgo.TextInputShort,
							Required:    true,
							MaxLength:   255,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleAddFeedTiktokPrompt(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalAddFeedTiktok,
			Title:    "Thêm kênh TikTok",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "channel_url",
							Label:       "Đường dẫn kênh TikTok",
							Placeholder: "https://www.tiktok.com/@username hoặc @username",
							Style:       discordgo.TextInputShort,
							Required:    true,
							MaxLength:   255,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleAddFeedYoutubeSubmit(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	var urlVal string
	if len(data.Components) > 0 {
		if row, ok := data.Components[0].(*discordgo.ActionsRow); ok && len(row.Components) > 0 {
			if input, ok := row.Components[0].(*discordgo.TextInput); ok {
				urlVal = strings.TrimSpace(input.Value)
			}
		}
	}

	state := getFeedSessionState(i.GuildID, i.Member.User.ID, i.ChannelID)
	targetCh := state.targetChannelID
	if targetCh == "" {
		targetCh = i.ChannelID
	}

	chID, chTitle, latestVID, _, err := feed.ResolveYouTubeChannel(urlVal)
	if err != nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Không thể thêm kênh vì %v", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	sub := &storage.VideoSubscription{
		GuildID:     i.GuildID,
		ChannelID:   targetCh,
		Platform:    "youtube",
		TargetID:    chID,
		TargetURL:   urlVal,
		TargetName:  chTitle,
		PingRoleID:  state.pingRoleID,
		LastVideoID: latestVID,
	}

	_ = s.store.AddVideoSubscription(sub)

	embed, components := s.buildFeedConfigView(i.GuildID, i.ChannelID, i.Member.User.ID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAddFeedTiktokSubmit(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	var urlVal string
	if len(data.Components) > 0 {
		if row, ok := data.Components[0].(*discordgo.ActionsRow); ok && len(row.Components) > 0 {
			if input, ok := row.Components[0].(*discordgo.TextInput); ok {
				urlVal = strings.TrimSpace(input.Value)
			}
		}
	}

	state := getFeedSessionState(i.GuildID, i.Member.User.ID, i.ChannelID)
	targetCh := state.targetChannelID
	if targetCh == "" {
		targetCh = i.ChannelID
	}

	username, displayName, latestVID, _, err := feed.ResolveTikTokChannel(urlVal)
	if err != nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Không thể thêm kênh vì %v", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	sub := &storage.VideoSubscription{
		GuildID:     i.GuildID,
		ChannelID:   targetCh,
		Platform:    "tiktok",
		TargetID:    username,
		TargetURL:   urlVal,
		TargetName:  displayName,
		PingRoleID:  state.pingRoleID,
		LastVideoID: latestVID,
	}

	_ = s.store.AddVideoSubscription(sub)

	embed, components := s.buildFeedConfigView(i.GuildID, i.ChannelID, i.Member.User.ID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleSelectDeleteFeed(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) > 0 {
		id, _ := strconv.Atoi(data.Values[0])
		_ = s.store.DeleteVideoSubscription(id)
	}

	embed, components := s.buildFeedConfigView(i.GuildID, i.ChannelID, i.Member.User.ID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) buildFeedConfigView(guildID, currentChannelID, userID string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	state := getFeedSessionState(guildID, userID, currentChannelID)
	subs, _ := s.store.GetVideoSubscriptionsByGuild(guildID)

	pingText := "Không ping"
	if state.pingRoleID != "" {
		pingText = fmt.Sprintf("<@&%s>", state.pingRoleID)
	}

	targetCh := state.targetChannelID
	if targetCh == "" {
		targetCh = currentChannelID
	}
	channelText := fmt.Sprintf("<#%s>", targetCh)

	var descLines []string
	descLines = append(descLines, fmt.Sprintf("Role ping chuẩn bị thêm **%s**", pingText))
	descLines = append(descLines, fmt.Sprintf("Kênh nhận thông báo **%s**", channelText))
	descLines = append(descLines, "")
	descLines = append(descLines, "**Danh sách kênh đang theo dõi**")

	if len(subs) == 0 {
		descLines = append(descLines, "Chưa có kênh nào được đăng ký theo dõi trong server")
	} else {
		for i, sub := range subs {
			pText := "Không ping"
			if sub.PingRoleID != "" {
				pText = fmt.Sprintf("<@&%s>", sub.PingRoleID)
			}
			plat := "YouTube"
			if sub.Platform == "tiktok" {
				plat = "TikTok"
			}
			descLines = append(descLines, fmt.Sprintf("%d. **[%s] %s**\nKênh nhận <#%s> | Ping %s", i+1, plat, sub.TargetName, sub.ChannelID, pText))
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Cấu hình thông báo video mới",
		Description: strings.Join(descLines, "\n"),
		Color:       0x3498db,
	}

	var components []discordgo.MessageComponent

	// Row 1: Thêm YouTube, Thêm TikTok, Không ping
	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Thêm YouTube",
				CustomID: BtnAddFeedYoutube,
				Style:    discordgo.SuccessButton,
			},
			discordgo.Button{
				Label:    "Thêm TikTok",
				CustomID: BtnAddFeedTiktok,
				Style:    discordgo.PrimaryButton,
			},
			discordgo.Button{
				Label:    "Không ping",
				CustomID: BtnFeedNoPing,
				Style:    discordgo.SecondaryButton,
			},
		},
	})

	// Row 2: Chọn role ping
	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				MenuType:    discordgo.RoleSelectMenu,
				CustomID:    SelectFeedRole,
				Placeholder: "Chọn vai trò nhắc",
			},
		},
	})

	// Row 3: Chọn kênh Discord nhận thông báo
	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				MenuType:     discordgo.ChannelSelectMenu,
				CustomID:     SelectFeedChannel,
				Placeholder:  "Chọn kênh nhận video",
				ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText},
			},
		},
	})

	// Row 4: Xóa kênh đang theo dõi (nếu có)
	if len(subs) > 0 {
		var delOptions []discordgo.SelectMenuOption
		for i, sub := range subs {
			if i >= 25 {
				break
			}
			plat := "YouTube"
			if sub.Platform == "tiktok" {
				plat = "TikTok"
			}
			label := fmt.Sprintf("[%s] %s", plat, sub.TargetName)
			if len(label) > 100 {
				label = label[:100]
			}
			delOptions = append(delOptions, discordgo.SelectMenuOption{
				Label:       label,
				Value:       fmt.Sprintf("%d", sub.ID),
				Description: fmt.Sprintf("Kênh #%s", sub.ChannelID),
			})
		}

		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    SelectDeleteFeed,
					Placeholder: "Chọn kênh cần xóa",
					Options:     delOptions,
				},
			},
		})
	}

	// Row cuối: Quay lại
	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Quay lại",
				CustomID: BtnBackToMain,
				Style:    discordgo.SecondaryButton,
			},
		},
	})

	return embed, components
}

func (s *Service) handleSettingSticky(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	if !s.isUserAdmin(sess, i.GuildID, i.Member.User.ID) {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Bạn cần quyền Administrator để thực hiện thao tác này",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	settings := s.store.GetChannelSettings(i.GuildID, i.ChannelID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalSettingSticky,
			Title:    "Cài đặt tin nhắn ghim",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    InputStickyContent,
							Label:       "Nội dung tin nhắn ghim",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "## Kênh này chỉ được gửi Video\n-# không được gửi\nĐể trống để tắt",
							Value:       settings.StickyContent,
							Required:    false,
							MaxLength:   2000,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleSubmitSettingSticky(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	var content string
	for _, row := range data.Components {
		if actionRow, ok := row.(*discordgo.ActionsRow); ok {
			for _, c := range actionRow.Components {
				if input, ok := c.(*discordgo.TextInput); ok && input.CustomID == InputStickyContent {
					content = input.Value
				}
			}
		}
	}

	err := s.stickyService.SetSticky(sess, i.GuildID, i.ChannelID, content)
	if err != nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Lỗi khi cài đặt tin nhắn ghim: %v", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	msgText := "Đã cài đặt tin nhắn ghim thành công"
	if strings.TrimSpace(content) == "" {
		msgText = "Đã tắt tin nhắn ghim tại kênh này"
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msgText,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (s *Service) handleViewRoleMenu(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	if !s.isUserAdmin(sess, i.GuildID, i.Member.User.ID) {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Bạn cần quyền Administrator để thực hiện thao tác này",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	minVal := 1
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "Bảng vai trò",
					Description: "Chọn vai trò đưa vào bảng",
					Color:       0x3498db,
				},
			},
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							MenuType:    discordgo.RoleSelectMenu,
							CustomID:    SelectRoleMenuRoles,
							Placeholder: "Chọn vai trò",
							MinValues:   &minVal,
							MaxValues:   25,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleSelectRoleMenuRoles(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	if !s.isUserAdmin(sess, i.GuildID, i.Member.User.ID) {
		return
	}

	data := i.MessageComponentData()
	roleIDs := data.Values
	if len(roleIDs) == 0 {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Vui lòng chọn ít nhất 1 role",
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	gRoles, err := sess.GuildRoles(i.GuildID)
	if err != nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Không thể tải danh sách role của server",
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	roleMap := make(map[string]*discordgo.Role)
	for _, r := range gRoles {
		roleMap[r.ID] = r
	}

	var validRoles []*discordgo.Role
	for _, rID := range roleIDs {
		if r, ok := roleMap[rID]; ok {
			if r.ID == i.GuildID || r.Managed {
				continue
			}
			validRoles = append(validRoles, r)
		}
	}

	if len(validRoles) == 0 {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Không có role nào hợp lệ để tạo menu role",
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	sort.Slice(validRoles, func(a, b int) bool {
		return validRoles[a].Position > validRoles[b].Position
	})

	if len(validRoles) > 25 {
		validRoles = validRoles[:25]
	}

	var options []discordgo.SelectMenuOption
	for _, r := range validRoles {
		options = append(options, discordgo.SelectMenuOption{
			Label:       r.Name,
			Value:       r.ID,
			Description: "Nhận hoặc bỏ role " + r.Name,
		})
	}

	zero := 0
	embed := &discordgo.MessageEmbed{
		Title:       "Nhận role",
		Description: "Chọn role bạn muốn nhận hoặc bỏ trong danh sách dưới đây",
		Color:       0xf1c40f,
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    RoleMenuAssign,
					Placeholder: "Chọn role muốn nhận hoặc bỏ...",
					MinValues:   &zero,
					MaxValues:   len(options),
					Options:     options,
				},
			},
		},
	}

	_, err = sess.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err != nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("Lỗi gửi bảng %v", err),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "Đã gửi bảng thành công",
			Embeds:     []*discordgo.MessageEmbed{},
			Components: []discordgo.MessageComponent{},
		},
	})
}

func (s *Service) handleRoleMenuAssign(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	if i.Member == nil || i.Member.User == nil {
		_, _ = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Không tìm thấy thông tin người dùng",
		})
		return
	}

	selectedIDs := i.MessageComponentData().Values
	selectedMap := make(map[string]bool)
	for _, id := range selectedIDs {
		selectedMap[id] = true
	}

	allOptionIDs := make(map[string]bool)
	if i.Message != nil {
		for _, row := range i.Message.Components {
			if actionRow, ok := row.(*discordgo.ActionsRow); ok {
				for _, c := range actionRow.Components {
					if sel, ok := c.(*discordgo.SelectMenu); ok && sel.CustomID == RoleMenuAssign {
						for _, opt := range sel.Options {
							allOptionIDs[opt.Value] = true
						}
					}
				}
			}
		}
	}

	member, err := sess.GuildMember(i.GuildID, i.Member.User.ID)
	if err != nil || member == nil {
		_, _ = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Không thể lấy thông tin thành viên từ server",
		})
		return
	}

	currentRoles := make(map[string]bool)
	for _, r := range member.Roles {
		currentRoles[r] = true
	}

	addedCount := 0
	removedCount := 0

	for rID := range allOptionIDs {
		want := selectedMap[rID]
		has := currentRoles[rID]

		if want && !has {
			if err := sess.GuildMemberRoleAdd(i.GuildID, member.User.ID, rID); err == nil {
				addedCount++
			}
		} else if !want && has {
			if err := sess.GuildMemberRoleRemove(i.GuildID, member.User.ID, rID); err == nil {
				removedCount++
			}
		}
	}

	var statusText string
	if addedCount > 0 || removedCount > 0 {
		statusText = fmt.Sprintf("Đã cập nhật vai trò nhận %d bỏ %d", addedCount, removedCount)
	} else {
		statusText = "Đã cập nhật vai trò không có thay đổi"
	}

	_, _ = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: statusText,
	})
}

func (s *Service) handleViewProfileAdmin(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	if !s.isUserAdmin(sess, i.GuildID, i.Member.User.ID) {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Bạn cần quyền Administrator để quản lý Profile",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if s.profileService == nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Dịch vụ Profile chưa khả dụng.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	embed, components := s.profileService.BuildProfileSelectMemberView()
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleViewWebsiteAdmin(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	if !s.isUserAdmin(sess, i.GuildID, i.Member.User.ID) {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Bạn cần quyền Administrator để quản lý Website",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if s.websiteService == nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Dịch vụ Website chưa khả dụng.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	embed, components := s.websiteService.BuildWebsiteAdminView(i.GuildID, i.ChannelID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleActionSyncCommands(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	if !s.isUserAdmin(sess, i.GuildID, i.Member.User.ID) {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Bạn cần quyền Administrator để đồng bộ lệnh.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	count, err := s.SyncCommands(sess, i.GuildID)
	if err != nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Lỗi đồng bộ lệnh %v", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Đã đồng bộ thành công %d lệnh", count),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (s *Service) handleSyncCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	if !s.isUserAdmin(sess, i.GuildID, userID) {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Bạn cần quyền Administrator để dùng lệnh này",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	count, err := s.SyncCommands(sess, i.GuildID)
	if err != nil {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Lỗi đồng bộ lệnh %v", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Đã đồng bộ thành công %d lệnh", count),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (s *Service) SyncCommands(sess *discordgo.Session, guildID string) (int, error) {
	if len(s.commands) == 0 {
		return 0, fmt.Errorf("danh sách commands đang trống")
	}

	appID := sess.State.User.ID

	// Xóa các lệnh cấp guild để tránh bị lặp đôi 2 lệnh giống hệt nhau
	if guildID != "" {
		_, _ = sess.ApplicationCommandBulkOverwrite(appID, guildID, []*discordgo.ApplicationCommand{})
	}

	// Đồng bộ danh sách lệnh Global chuẩn
	_, err := sess.ApplicationCommandBulkOverwrite(appID, "", s.commands)
	if err != nil {
		return 0, err
	}
	return len(s.commands), nil
}


