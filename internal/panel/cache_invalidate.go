package panel

func (h *Handler) invalidateAuthCache() {
	if h.AuthCache != nil {
		h.AuthCache.InvalidateAll()
	}
}
