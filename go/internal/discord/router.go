package discord

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

type CommandHandler func(s *discordgo.Session, i *discordgo.InteractionCreate)

type ComponentHandler func(s *discordgo.Session, i *discordgo.InteractionCreate)

type ModalHandler func(s *discordgo.Session, i *discordgo.InteractionCreate)

type Router struct {
	commands          map[string]CommandHandler
	components        map[string]ComponentHandler
	modals            map[string]ModalHandler
	componentPrefixes map[string]ComponentHandler
	modalPrefixes     map[string]ModalHandler
}

func NewRouter() *Router {
	return &Router{
		commands:          make(map[string]CommandHandler),
		components:        make(map[string]ComponentHandler),
		modals:            make(map[string]ModalHandler),
		componentPrefixes: make(map[string]ComponentHandler),
		modalPrefixes:     make(map[string]ModalHandler),
	}
}

func (r *Router) RegisterCommand(name string, handler CommandHandler) {
	r.commands[name] = handler
}

func (r *Router) RegisterComponent(customID string, handler ComponentHandler) {
	r.components[customID] = handler
}

func (r *Router) RegisterComponentPrefix(prefix string, handler ComponentHandler) {
	r.componentPrefixes[prefix] = handler
}

func (r *Router) RegisterModal(customID string, handler ModalHandler) {
	r.modals[customID] = handler
}

func (r *Router) RegisterModalPrefix(prefix string, handler ModalHandler) {
	r.modalPrefixes[prefix] = handler
}

func (r *Router) HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		data := i.ApplicationCommandData()
		if handler, ok := r.commands[data.Name]; ok {
			handler(s, i)
		} else {
			log.Printf("Chưa đăng ký handler cho lệnh: /%s", data.Name)
		}

	case discordgo.InteractionMessageComponent:
		data := i.MessageComponentData()
		if handler, ok := r.components[data.CustomID]; ok {
			handler(s, i)
		} else {
			handled := false
			for prefix, h := range r.componentPrefixes {
				if len(data.CustomID) >= len(prefix) && data.CustomID[:len(prefix)] == prefix {
					h(s, i)
					handled = true
					break
				}
			}
			if !handled {
				log.Printf("Chưa đăng ký handler cho component: %s", data.CustomID)
			}
		}

	case discordgo.InteractionModalSubmit:
		data := i.ModalSubmitData()
		if handler, ok := r.modals[data.CustomID]; ok {
			handler(s, i)
		} else {
			handled := false
			for prefix, h := range r.modalPrefixes {
				if len(data.CustomID) >= len(prefix) && data.CustomID[:len(prefix)] == prefix {
					h(s, i)
					handled = true
					break
				}
			}
			if !handled {
				log.Printf("Chưa đăng ký handler cho modal: %s", data.CustomID)
			}
		}
	}
}
