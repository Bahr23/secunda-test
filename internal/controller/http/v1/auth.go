package v1

import "net/http"

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := h.auth.Register(r.Context(), in.Email, in.Password, in.Name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	token, err := h.auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "Bearer"})
}
