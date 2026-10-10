package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type notificationHandler struct {
	svc *service.NotificationService
}

// NewNotification 提醒处理器（specs/notifications，任务 10.2；对任意登录角色开放）。
func NewNotification(svc *service.NotificationService) *notificationHandler {
	return &notificationHandler{svc: svc}
}

// List GET /api/v1/notifications 列表（新的在前）+ 未读数，铃铛与列表页共用一次请求。
func (h *notificationHandler) List(c *gin.Context) {
	list, unread, err := h.svc.ForUser(middleware.CurrentUser(c)).List()
	if err != nil {
		response.Internal(c, "读取提醒失败")
		return
	}
	response.OK(c, gin.H{"list": list, "unread": unread})
}

// MarkRead PATCH /api/v1/notifications/:id 单条已读（幂等；已读仍留在列表可回看）。
func (h *notificationHandler) MarkRead(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	unread, err := h.svc.ForUser(middleware.CurrentUser(c)).MarkRead(id)
	if err != nil {
		if errors.Is(err, service.ErrNotifNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.Internal(c, "操作失败")
		return
	}
	response.OK(c, gin.H{"id": id, "unread": unread})
}

// MarkAllRead POST /api/v1/notifications/read-all 一键全部已读（未读归零，红点消失）。
func (h *notificationHandler) MarkAllRead(c *gin.Context) {
	unread, err := h.svc.ForUser(middleware.CurrentUser(c)).MarkAllRead()
	if err != nil {
		response.Internal(c, "操作失败")
		return
	}
	response.OK(c, gin.H{"unread": unread})
}
