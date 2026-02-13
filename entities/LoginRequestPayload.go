package entities

type LoginRequestPayload struct {
   FirstName  string `json:"first_name"`
   LastName string `json:"last_name"`
   Email string `json:"email"`
   Password string `json:"password"`
}

type LoginResponsePayload struct {
   MainToken string `json:"main_token"`
   RefreshToken string `json:"refresh_token"`
}

type SignupRequestPayload struct {
   FirstName  string `json:"first_name"`
   LastName string `json:"last_name"`
   Email string `json:"email"`
   Password string `json:"password"`
}

type SignupResponsePayload struct {
   MainToken string `json:"main_token"`
   RefreshToken string `json:"refresh_token"`
}
