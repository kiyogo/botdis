package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"botdis/config"
	"botdis/internal/discord"
	"botdis/internal/modules/autoroles"
	"botdis/internal/modules/chatai"
	"botdis/internal/modules/dashboard"
	"botdis/internal/modules/feed"
	"botdis/internal/modules/leveling"
	"botdis/internal/modules/moderation"
	"botdis/internal/modules/profile"
	"botdis/internal/modules/sticky"
	"botdis/internal/modules/ticket"
	"botdis/internal/modules/tiktok"
	"botdis/internal/modules/utility"
	"botdis/internal/modules/website"
	"botdis/internal/modules/wordchain"
	"botdis/internal/storage"

	"github.com/bwmarrin/discordgo"
)

func main() {
	// Khóa đơn tiến trình (Single-Instance Lock): Chống chạy đè 2 bot dẫn tới trả lời lặp 2 lần
	lockListener, err := net.Listen("tcp", "127.0.0.1:28472")
	if err != nil {
		log.Fatalf("CẢNH BÁO: Đã có một tiến trình botdis.exe khác đang hoạt động! Đang dừng để tránh trả lời lặp 2 lần tin nhắn.")
	}
	defer lockListener.Close()

	log.Println("Đang khởi động Bot Discord bằng Golang...")

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Lỗi nạp cấu hình: %v", err)
	}

	store, err := storage.OpenMySQL(cfg.MySQL)
	if err != nil {
		log.Fatalf("Lỗi kết nối MySQL: %v", err)
	}
	defer store.Close()

	sess, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		log.Fatalf("Lỗi tạo Discord session: %v", err)
	}

	router := discord.NewRouter()
	tiktokService := tiktok.NewService(store)
	antiSpam := moderation.NewAntiSpam(store)
	levelingService := leveling.NewService(store)
	utilityService := utility.NewService()
	autorolesService := autoroles.NewService(store)
	stickyService := sticky.NewService(store)
	profileService := profile.NewService(store)
	websiteService := website.NewService(store, sess)
	defer websiteService.Stop()

	dashboardService := dashboard.NewService(store, stickyService, profileService, websiteService)
	chataiService := chatai.NewService(cfg.AIAPIKey, cfg.LocalAIURL, store)
	ticketService := ticket.NewService(store, chataiService)
	feedService := feed.NewService(store)
	defer feedService.Stop()

	dict, err := wordchain.NewDictionary("dictionary.db")
	if err != nil {
		log.Printf("Cảnh báo: Không thể nạp từ điển dictionary.db: %v", err)
	} else {
		defer dict.Close()
	}
	wordchainService := wordchain.NewService(store, dict)

	ticketService.RegisterRoutes(router)
	levelingService.RegisterRoutes(router)
	utilityService.RegisterRoutes(router)
	autorolesService.RegisterRoutes(router)
	profileService.RegisterRoutes(router)
	websiteService.RegisterRoutes(router)
	dashboardService.RegisterRoutes(router)
	wordchainService.RegisterRoutes(router)
	antiSpam.RegisterCommands(router)

	sess.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsGuildMessageTyping |
		discordgo.IntentsMessageContent

	sess.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Đã đăng nhập với tư cách: %s#%s (ID: %s)", r.User.Username, r.User.Discriminator, r.User.ID)
		if cfg.StatusMessage != "" {
			_ = s.UpdateGameStatus(0, cfg.StatusMessage)
		}

		commands := []*discordgo.ApplicationCommand{
			{
				Name:        "setting",
				Description: "Bảng điều khiển cài đặt",
			},
			{
				Name:        "profile",
				Description: "Xem profile của một thành viên hoặc bản thân",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "Thành viên trong server cần xem profile",
						Required:    false,
					},
				},
			},
			{
				Name:        "sync",
				Description: "Admin đồng bộ lại slash commands cho server và global",
			},
			{
				Name:        "noitu",
				Description: "Bảng điều khiển nối từ",
			},
			{
				Name:        "tratu",
				Description: "Tra cứu từ điển tiếng Việt",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "tu",
						Description: "Từ muốn tra cứu",
						Required:    true,
					},
				},
			},
			{
				Name:        "rank",
				Description: "Xem cấp độ và điểm kinh nghiệm",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "Người dùng cần xem (mặc định là bản thân)",
						Required:    false,
					},
				},
			},
			{
				Name:        "clear",
				Description: "Xóa tin nhắn hàng loạt trong kênh",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Số lượng tin nhắn cần xóa (1 - 100)",
						Required:    false,
					},
				},
			},
			{
				Name:        "mute",
				Description: "Mute (Timeout) thành viên",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "Thành viên cần Mute",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "minutes",
						Description: "Thời gian Mute (phút)",
						Required:    false,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "reason",
						Description: "Lý do Mute",
						Required:    false,
					},
				},
			},
			{
				Name:        "unmute",
				Description: "Bỏ Mute (Timeout) cho thành viên",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "Thành viên cần bỏ Mute",
						Required:    true,
					},
				},
			},
			{
				Name:        "kick",
				Description: "Kick thành viên khỏi server",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "Thành viên cần Kick",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "reason",
						Description: "Lý do Kick",
						Required:    false,
					},
				},
			},
			{
				Name:        "ban",
				Description: "Ban thành viên khỏi server",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "Thành viên cần Ban",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "reason",
						Description: "Lý do Ban",
						Required:    false,
					},
				},
			},
		}

		dashboardService.SetCommands(commands)

		// Dọn sạch các lệnh cấp server (guild commands) cũ để tránh bị lặp 2 lệnh giống hệt nhau
		guilds, err := s.UserGuilds(100, "", "", false)
		if err == nil {
			for _, g := range guilds {
				_, _ = s.ApplicationCommandBulkOverwrite(s.State.User.ID, g.ID, []*discordgo.ApplicationCommand{})
			}
		}

		_, err = s.ApplicationCommandBulkOverwrite(s.State.User.ID, "", commands)
		if err != nil {
			log.Printf("Lỗi đồng bộ slash commands: %v", err)
		} else {
			log.Printf("Đã đồng bộ thành công %d commands!", len(commands))
		}
	})

	sess.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		router.HandleInteraction(s, i)
	})

	sess.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		autorolesService.HandleGuildMemberAdd(s, m)
	})

	sess.AddHandler(func(s *discordgo.Session, ch *discordgo.ChannelDelete) {
		ticketService.HandleChannelDelete(s, ch)
	})

	sess.AddHandler(func(s *discordgo.Session, t *discordgo.TypingStart) {
		stickyService.HandleTypingStart(s, t)
	})

	sess.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot {
			return
		}

		stickyService.HandleMessage(s, m)

		violated := antiSpam.HandleMessage(s, m)
		if violated {
			return
		}

		levelingService.HandleMessage(s, m)

		tiktokService.HandleMessage(s, m)

		chataiService.HandleMessage(s, m)

		wordchainService.HandleMessage(s, m)
	})

	err = sess.Open()
	if err != nil {
		log.Fatalf("Lỗi mở kết nối Discord: %v", err)
	}
	defer sess.Close()

	ticketService.StartAutoDeleteWorker(sess)
	feedService.Start(sess)

	log.Println("Bot Go đang hoạt động! Nhấn Ctrl+C để dừng.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	fmt.Println("\nĐang tắt Bot Go...")
}
