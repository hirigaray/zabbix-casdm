package handlers

import (
	"database/sql"
	"html/template"
	"net/http"
)

// Event é a struct que descreve uma linha da tabela Events.
type Event struct {
	ID       int    `json:"id"`
	ZabbixID int    `json:"zabbix_id"`
	CAID     int    `json:"ca_id"`
	CAStatus string `json:"ca_status"`
}

// Trigger é a struct que descreve uma linha da tabela Triggers.
type Trigger struct {
	ID                 int    `json:"id"`
	HostID             int    `json:"host_id"`
	HostName           string `json:"host_name"`
	TriggerID          int    `json:"trigger_id"`
	TriggerDescription string `json:"trigger_description"`
	SendToCA           bool   `json:"send_to_ca"`
}

// Listar todos os eventos.
func ListEvents(db *sql.DB) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "O método GET não é permitido.", http.StatusBadRequest)
			return
		}

		// Selecionar todos os eventos registrados na database.
		rows, err := db.Query("SELECT * FROM events")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var events []Event
		var event Event

		// Escanear e transformar em estruturas para que sejam visíveis ao html template.
		for rows.Next() {
			err = rows.Scan(&event.ID, &event.ZabbixID, &event.CAID, &event.CAStatus)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			events = append(events, event)
		}
		rows.Close()

		t, err := template.New("list-events.html").ParseFiles("html/list-events.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Executar o template com a estrutura de eventos para que a lista seja construida no html.
		err = t.Execute(w, events)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
}

// Listar todos os triggers.
func ListTriggers(db *sql.DB) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "O método GET não é permitido.", http.StatusBadRequest)
			return
		}

		rows, err := db.Query("SELECT * FROM triggers")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var triggers []Trigger
		var trigger Trigger

		for rows.Next() {
			err = rows.Scan(&trigger.ID,
				&trigger.HostID,
				&trigger.HostName,
				&trigger.TriggerID,
				&trigger.TriggerDescription,
				&trigger.SendToCA)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			triggers = append(triggers, trigger)
		}
		rows.Close()

		t, err := template.New("list-triggers.html").ParseFiles("html/list-triggers.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = t.Execute(w, triggers)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
}

// Envia os triggers à database.
func PostTriggers(db *sql.DB) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Desabilitar todos os triggers a serem enviados ao CA.
		_, err := db.Exec("UPDATE triggers SET send_to_ca='f'")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.ParseForm()
		if len(r.Form) > 0 {
			// Recriar lista, a partir do formulário HTML.
			query := "WHERE trigger_id=" + r.Form["send_to_ca"][0]
			for _, s := range r.Form["send_to_ca"][1:] {
				query = query + "or trigger_id=" + s
			}
			_, err := db.Exec("UPDATE triggers SET send_to_ca='t'" + query)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		// Redirecionar à lista de triggers.
		http.Redirect(w, r, r.Header.Get("Referer"), 302)
	})
}
