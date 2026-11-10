package profile

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"botdis/internal/discord"
	"botdis/internal/storage"

	"github.com/bwmarrin/discordgo"
)

type Service struct {
	store *storage.MySQLStore
}

func NewService(store *storage.MySQLStore) *Service {
	return &Service{store: store}
}

func capitalizeFirst(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func (s *Service) RegisterRoutes(r *discord.Router) {
	r.RegisterCommand("profile", s.handleProfileCommand)

	// Detail button
	r.RegisterComponentPrefix(BtnProfileDetailPrefix, s.handleProfileDetail)

	// Admin selection & views
	r.RegisterComponent(SelectProfileMember, s.handleSelectMember)
	r.RegisterComponentPrefix(BtnProfileAdminVerifyPrefix, s.handleAdminVerifyToggle)
	r.RegisterComponentPrefix(BtnProfileAdminEditInfoPrefix, s.handleAdminEditInfoView)
	r.RegisterComponentPrefix(BtnProfileAdminEditStatusPrefix, s.handleAdminStatusModalPrompt)
	r.RegisterModalPrefix(ModalProfileEditStatusPrefix, s.handleAdminStatusModalSubmit)
	r.RegisterComponentPrefix(BtnProfileAdminAddFieldPrefix, s.handleAdminAddFieldModalPrompt)
	r.RegisterModalPrefix(ModalProfileAddFieldPrefix, s.handleAdminAddFieldModalSubmit)
	r.RegisterComponentPrefix(SelectProfileEditFieldPrefix, s.handleAdminSelectEditField)
	r.RegisterModalPrefix(ModalProfileEditFieldValuePrefix, s.handleAdminEditFieldValueModalSubmit)
	r.RegisterComponentPrefix(BtnProfileAdminDeleteFieldPrefix, s.handleAdminDeleteFieldView)
	r.RegisterComponentPrefix(SelectProfileDeleteFieldPrefix, s.handleAdminSelectDeleteField)
	r.RegisterComponentPrefix(BtnProfileAdminResetPrefix, s.handleAdminResetProfile)
	r.RegisterComponentPrefix(BtnProfileAdminBackToMemPrefix, s.handleAdminBackToMember)
}

func (s *Service) handleProfileCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	targetUser := i.Member.User
	if i.User != nil {
		targetUser = i.User
	}

	options := i.ApplicationCommandData().Options
	if len(options) > 0 && options[0].Name == "user" && options[0].UserValue(sess) != nil {
		targetUser = options[0].UserValue(sess)
	}

	embed, components := s.BuildProfileCard(sess, i.GuildID, targetUser)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) BuildProfileCard(sess *discordgo.Session, guildID string, targetUser *discordgo.User) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	prof, err := s.store.GetProfile(targetUser.ID)
	if err != nil {
		prof = &storage.UserProfile{
			UserID:       targetUser.ID,
			Status:       "Không xác định",
			Verify:       false,
			CustomFields: make(map[string]string),
		}
	}

	idInt, _ := strconv.ParseInt(targetUser.ID, 10, 64)
	createdAt := time.UnixMilli((idInt >> 22) + 1420070400000)
	createdStr := createdAt.Format("02/01/2006")

	member, memberErr := sess.GuildMember(guildID, targetUser.ID)
	memberFound := memberErr == nil && member != nil

	joinedStr := "Không rõ"
	if memberFound && !member.JoinedAt.IsZero() {
		joinedStr = member.JoinedAt.Format("02/01/2006")
	} else if !memberFound {
		joinedStr = "Không có"
	}

	var allowedRoleMentions []string
	if memberFound {
		allowedRoleIDs, _ := s.store.GetGuildProfileRoles(guildID)
		allowedMap := make(map[string]bool)
		for _, rID := range allowedRoleIDs {
			allowedMap[rID] = true
		}

		guild, _ := sess.Guild(guildID)
		roleMap := make(map[string]*discordgo.Role)
		if guild != nil {
			for _, r := range guild.Roles {
				roleMap[r.ID] = r
			}
		}

		var memberRoles []*discordgo.Role
		for _, rID := range member.Roles {
			if r, ok := roleMap[rID]; ok && r.Name != "@everyone" {
				if len(allowedMap) == 0 || allowedMap[r.ID] {
					memberRoles = append(memberRoles, r)
				}
			}
		}
		sort.Slice(memberRoles, func(i, j int) bool {
			return memberRoles[i].Position > memberRoles[j].Position
		})
		for _, r := range memberRoles {
			allowedRoleMentions = append(allowedRoleMentions, r.Mention())
		}
	}

	rolesText := "Không có"
	if len(allowedRoleMentions) > 0 {
		rolesText = strings.Join(allowedRoleMentions, " ")
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Hồ sơ %s", targetUser.Username),
		Color: 0x3498db,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: targetUser.AvatarURL("256"),
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Thành viên", Value: targetUser.Mention(), Inline: true},
			{Name: "Id", Value: targetUser.ID, Inline: true},
			{Name: "\u200b", Value: "\u200b", Inline: false},
			{Name: "Tham gia", Value: joinedStr, Inline: true},
			{Name: "Tạo tài khoản", Value: createdStr, Inline: true},
			{Name: "Vai trò", Value: rolesText, Inline: false},
			{Name: "Trạng thái", Value: prof.Status, Inline: false},
		},
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Chi tiết",
					CustomID: BtnProfileDetailPrefix + targetUser.ID,
					Style:    discordgo.PrimaryButton,
				},
			},
		},
	}

	return embed, components
}

func (s *Service) handleProfileDetail(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileDetailPrefix)

	prof, err := s.store.GetProfile(targetUserID)
	if err != nil {
		prof = &storage.UserProfile{
			UserID:       targetUserID,
			Status:       "Không xác định",
			Verify:       false,
			CustomFields: make(map[string]string),
		}
	}

	targetUser, err := sess.User(targetUserID)
	userName := targetUserID
	var avatarURL string
	if err == nil && targetUser != nil {
		userName = targetUser.Username
		avatarURL = targetUser.AvatarURL("256")
	}

	verifyText := "Chưa xác thực"
	if prof.Verify {
		verifyText = "Đã xác thực"
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Chi tiết %s", userName),
		Color: 0x5865F2,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: avatarURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Trạng thái", Value: prof.Status, Inline: false},
			{Name: "Xác thực", Value: verifyText, Inline: false},
		},
	}

	var fieldKeys []string
	for k := range prof.CustomFields {
		fieldKeys = append(fieldKeys, k)
	}
	sort.Strings(fieldKeys)

	for _, k := range fieldKeys {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   capitalizeFirst(k),
			Value:  prof.CustomFields[k],
			Inline: false,
		})
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

// --- Profile Admin Logic ---

func (s *Service) BuildProfileSelectMemberView() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	embed := &discordgo.MessageEmbed{
		Title:       "Hồ sơ",
		Description: "Chọn thành viên",
		Color:       0x5865F2,
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.UserSelectMenu,
					CustomID:    SelectProfileMember,
					Placeholder: "Chọn thành viên",
					MinValues:   nil,
					MaxValues:   1,
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnProfileAdminBackToDashboard,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}

	return embed, components
}

func (s *Service) BuildProfileAdminCard(sess *discordgo.Session, targetUserID string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	prof, _ := s.store.GetProfile(targetUserID)
	if prof == nil {
		prof = &storage.UserProfile{
			UserID:       targetUserID,
			Status:       "Không xác định",
			Verify:       false,
			CustomFields: make(map[string]string),
		}
	}

	targetUser, _ := sess.User(targetUserID)
	userName := targetUserID
	var avatarURL string
	if targetUser != nil {
		userName = targetUser.Username
		avatarURL = targetUser.AvatarURL("256")
	}

	verifyStatus := "Chưa xác thực"
	if prof.Verify {
		verifyStatus = "Đã xác thực"
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Hồ sơ %s", userName),
		Color: 0x3498db,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: avatarURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Id", Value: targetUserID, Inline: true},
			{Name: "Xác thực", Value: verifyStatus, Inline: true},
			{Name: "Trạng thái", Value: prof.Status, Inline: false},
		},
	}

	var fieldKeys []string
	for k := range prof.CustomFields {
		fieldKeys = append(fieldKeys, k)
	}
	sort.Strings(fieldKeys)

	for _, k := range fieldKeys {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   capitalizeFirst(k),
			Value:  prof.CustomFields[k],
			Inline: false,
		})
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Sửa",
					CustomID: BtnProfileAdminEditInfoPrefix + targetUserID,
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					Label:    "Xác thực",
					CustomID: BtnProfileAdminVerifyPrefix + targetUserID,
					Style:    discordgo.PrimaryButton,
				},
				discordgo.Button{
					Label:    "Chọn lại",
					CustomID: SelectProfileMember,
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnProfileAdminBackToDashboard,
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}

	return embed, components
}

func (s *Service) BuildProfileEditInfoComponents(prof *storage.UserProfile) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent

	if len(prof.CustomFields) > 0 {
		var options []discordgo.SelectMenuOption
		var keys []string
		for k := range prof.CustomFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := prof.CustomFields[k]
			desc := v
			if len(desc) > 50 {
				desc = desc[:50]
			}
			options = append(options, discordgo.SelectMenuOption{
				Label:       capitalizeFirst(k),
				Value:       k,
				Description: desc,
			})
			if len(options) >= 25 {
				break
			}
		}

		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    SelectProfileEditFieldPrefix + prof.UserID,
					Placeholder: "Chọn trường",
					Options:     options,
				},
			},
		})
	}

	// Buttons
	rows = append(rows, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Thêm",
				CustomID: BtnProfileAdminAddFieldPrefix + prof.UserID,
				Style:    discordgo.SuccessButton,
			},
			discordgo.Button{
				Label:    "Xóa",
				CustomID: BtnProfileAdminDeleteFieldPrefix + prof.UserID,
				Style:    discordgo.DangerButton,
			},
			discordgo.Button{
				Label:    "Trạng thái",
				CustomID: BtnProfileAdminEditStatusPrefix + prof.UserID,
				Style:    discordgo.PrimaryButton,
			},
			discordgo.Button{
				Label:    "Đặt lại",
				CustomID: BtnProfileAdminResetPrefix + prof.UserID,
				Style:    discordgo.DangerButton,
			},
		},
	})

	// Row 2: Back button
	rows = append(rows, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Quay lại",
				CustomID: BtnProfileAdminBackToMemPrefix + prof.UserID,
				Style:    discordgo.SecondaryButton,
			},
		},
	})

	return rows
}

func (s *Service) handleSelectMember(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		embed, components := s.BuildProfileSelectMemberView()
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
		return
	}

	targetUserID := data.Values[0]
	embed, components := s.BuildProfileAdminCard(sess, targetUserID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminVerifyToggle(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileAdminVerifyPrefix)

	_, err := s.store.ToggleProfileVerify(targetUserID)
	if err != nil {
		log.Printf("Lỗi toggle verify: %v", err)
	}

	embed, components := s.BuildProfileAdminCard(sess, targetUserID)
	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminEditInfoView(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileAdminEditInfoPrefix)

	prof, _ := s.store.GetProfile(targetUserID)
	if prof == nil {
		prof = &storage.UserProfile{UserID: targetUserID, Status: "Không xác định", CustomFields: make(map[string]string)}
	}

	embed, _ := s.BuildProfileAdminCard(sess, targetUserID)
	components := s.BuildProfileEditInfoComponents(prof)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminStatusModalPrompt(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileAdminEditStatusPrefix)

	prof, _ := s.store.GetProfile(targetUserID)
	currentStatus := ""
	if prof != nil {
		currentStatus = prof.Status
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalProfileEditStatusPrefix + targetUserID,
			Title:    "Trạng thái",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "status_input",
							Label:       "Trạng thái",
							Style:       discordgo.TextInputShort,
							Placeholder: "Trạng thái",
							Value:       currentStatus,
							MaxLength:   100,
							Required:    true,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleAdminStatusModalSubmit(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	targetUserID := strings.TrimPrefix(data.CustomID, ModalProfileEditStatusPrefix)

	newStatus := ""
	for _, row := range data.Components {
		if ar, ok := row.(*discordgo.ActionsRow); ok {
			for _, comp := range ar.Components {
				if ti, ok := comp.(*discordgo.TextInput); ok && ti.CustomID == "status_input" {
					newStatus = strings.TrimSpace(ti.Value)
				}
			}
		}
	}

	if newStatus != "" {
		_ = s.store.UpdateProfileStatus(targetUserID, newStatus)
	}

	prof, _ := s.store.GetProfile(targetUserID)
	embed, _ := s.BuildProfileAdminCard(sess, targetUserID)
	components := s.BuildProfileEditInfoComponents(prof)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminAddFieldModalPrompt(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileAdminAddFieldPrefix)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalProfileAddFieldPrefix + targetUserID,
			Title:    "Thêm trường",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "field_name",
							Label:       "Tên trường",
							Style:       discordgo.TextInputShort,
							Placeholder: "Tên trường",
							MinLength:   1,
							MaxLength:   50,
							Required:    true,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "field_value",
							Label:       "Giá trị",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "Giá trị",
							Required:    true,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleAdminAddFieldModalSubmit(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	targetUserID := strings.TrimPrefix(data.CustomID, ModalProfileAddFieldPrefix)

	fieldName := ""
	fieldVal := ""

	for _, row := range data.Components {
		if ar, ok := row.(*discordgo.ActionsRow); ok {
			for _, comp := range ar.Components {
				if ti, ok := comp.(*discordgo.TextInput); ok {
					switch ti.CustomID {
					case "field_name":
						fieldName = strings.ToLower(strings.TrimSpace(ti.Value))
					case "field_value":
						fieldVal = strings.TrimSpace(ti.Value)
					}
				}
			}
		}
	}

	if fieldName != "" && fieldVal != "" {
		_ = s.store.SetProfileCustomField(targetUserID, fieldName, fieldVal)
	}

	prof, _ := s.store.GetProfile(targetUserID)
	embed, _ := s.BuildProfileAdminCard(sess, targetUserID)
	components := s.BuildProfileEditInfoComponents(prof)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminSelectEditField(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	targetUserID := strings.TrimPrefix(data.CustomID, SelectProfileEditFieldPrefix)

	if len(data.Values) == 0 {
		return
	}
	fieldName := data.Values[0]

	prof, _ := s.store.GetProfile(targetUserID)
	currentVal := ""
	if prof != nil && prof.CustomFields != nil {
		currentVal = prof.CustomFields[fieldName]
	}

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ModalProfileEditFieldValuePrefix + targetUserID + "___" + fieldName,
			Title:    "Sửa trường",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "field_name",
							Label:     "Tên trường",
							Style:     discordgo.TextInputShort,
							Value:     fieldName,
							Required:  true,
							MaxLength: 50,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "field_value",
							Label:     "Giá trị",
							Style:     discordgo.TextInputParagraph,
							Value:     currentVal,
							Required:  true,
							MaxLength: 1000,
						},
					},
				},
			},
		},
	})
}

func (s *Service) handleAdminEditFieldValueModalSubmit(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	payload := strings.TrimPrefix(data.CustomID, ModalProfileEditFieldValuePrefix)
	parts := strings.Split(payload, "___")
	if len(parts) != 2 {
		return
	}
	targetUserID := parts[0]
	oldFieldName := parts[1]

	newFieldName := ""
	newVal := ""
	for _, row := range data.Components {
		if ar, ok := row.(*discordgo.ActionsRow); ok {
			for _, comp := range ar.Components {
				if ti, ok := comp.(*discordgo.TextInput); ok {
					switch ti.CustomID {
					case "field_name":
						newFieldName = strings.ToLower(strings.TrimSpace(ti.Value))
					case "field_value":
						newVal = strings.TrimSpace(ti.Value)
					}
				}
			}
		}
	}

	if newFieldName != "" && newVal != "" {
		if newFieldName != oldFieldName {
			_ = s.store.DeleteProfileCustomField(targetUserID, oldFieldName)
		}
		_ = s.store.SetProfileCustomField(targetUserID, newFieldName, newVal)
	}

	prof, _ := s.store.GetProfile(targetUserID)
	embed, _ := s.BuildProfileAdminCard(sess, targetUserID)
	components := s.BuildProfileEditInfoComponents(prof)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminDeleteFieldView(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileAdminDeleteFieldPrefix)

	prof, _ := s.store.GetProfile(targetUserID)
	if prof == nil || len(prof.CustomFields) == 0 {
		_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Không có trường để xóa",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	var options []discordgo.SelectMenuOption
	var keys []string
	for k := range prof.CustomFields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		options = append(options, discordgo.SelectMenuOption{
			Label: capitalizeFirst(k),
			Value: k,
		})
		if len(options) >= 25 {
			break
		}
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    SelectProfileDeleteFieldPrefix + targetUserID,
					Placeholder: "Chọn trường",
					Options:     options,
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Quay lại",
					CustomID: BtnProfileAdminEditInfoPrefix + targetUserID,
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

func (s *Service) handleAdminSelectDeleteField(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	targetUserID := strings.TrimPrefix(data.CustomID, SelectProfileDeleteFieldPrefix)

	if len(data.Values) > 0 {
		fieldName := data.Values[0]
		_ = s.store.DeleteProfileCustomField(targetUserID, fieldName)
	}

	prof, _ := s.store.GetProfile(targetUserID)
	embed, _ := s.BuildProfileAdminCard(sess, targetUserID)
	components := s.BuildProfileEditInfoComponents(prof)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminResetProfile(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileAdminResetPrefix)

	_ = s.store.ResetProfile(targetUserID)

	embed, components := s.BuildProfileAdminCard(sess, targetUserID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func (s *Service) handleAdminBackToMember(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	targetUserID := strings.TrimPrefix(customID, BtnProfileAdminBackToMemPrefix)

	embed, components := s.BuildProfileAdminCard(sess, targetUserID)

	_ = sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}
