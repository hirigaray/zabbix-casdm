package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kori/zabbix-casdm/handlers"

	"github.com/cavaliercoder/go-zabbix"
	"github.com/kori/go-casdm"
	_ "github.com/lib/pq"
)

// Sessions é a struct que agrupa todas as sessões abertas nos diferentes serviços.
type Sessions struct {
	DB     *sql.DB
	Zabbix *zabbix.Session
	CASDM  casdm.Session
}

func (s Sessions) CleanUpHosts(l *log.Logger) {
	for range time.Tick(time.Duration(5) * time.Second) {
		// Pesquisar e remover hosts da database que não estão mais no Zabbix.
		if hosts, err := s.Zabbix.GetHosts(zabbix.HostGetParams{MonitoredOnly: true}); err == nil {
			if len(hosts) > 0 {
				// Criar condição para o delete.
				hostsQuery := "WHERE host_id = " + hosts[0].HostID
				for _, host := range hosts[1:] {
					// Adicionar IDs dos hosts, um por um, numa frase do formato:
					// "or host_id1 or host_id2 or host_id3", etc.
					hostsQuery = hostsQuery + " or host_id = " + host.HostID
				}
				sq := "(SELECT DISTINCT host_id FROM triggers " + hostsQuery + ")"
				rows, err := s.DB.Query(`DELETE FROM triggers WHERE host_id NOT IN` + sq)
				if err != nil {
					log.Println(err)
				}
				rows.Close()
			}
		}
	}
}

func NewSessions(caURL string, zabbixURL string, postgresURL string, l *log.Logger) Sessions {
	l.Println("Postgres: Conectando...")
	postgresSession, err := sql.Open("postgres", postgresURL)
	if err != nil {
		l.Fatal("Postgres:", err)
	} else {
		l.Println("Postgres: Conexão estabelecida.")
		l.Println("Criando as tabelas necessárias, se elas ainda não existem...")
		rows, err := postgresSession.Query(`
		create table if not exists events (
			id        serial   primary key,
			zabbix_id integer  not null unique,
			ca_id     integer           unique,
			ca_status char(15)
		);`)
		if err != nil {
			l.Println(err)
		}
		rows.Close()

		rows, err = postgresSession.Query(`
	create table if not exists triggers (
			id                  serial    primary key,
			host_id             integer   not null,
			host_name           char(30)  not null,
			trigger_id          integer   not null unique,
			trigger_description char(100) not null,
			send_to_ca          bool      not null
		);`)
		if err != nil {
			l.Println(err)
		}
		rows.Close()
	}

	// Iniciar uma sessão do Zabbix.
	l.Println("Zabbix: Conectando...")
	zabbixSession, err := zabbix.NewSession(zabbixURL+"/api_jsonrpc.php", "admin", "xxxx")
	if err != nil {
		l.Fatal("Zabbix:", err)
	} else {
		l.Println("Zabbix: Conexão estabelecida.")
	}

	// Iniciar uma sessão do CA.
	l.Println("CA: Conectando...")
	caSession, err := casdm.NewSession(caURL, "")
	if err != nil {
		l.Fatal("CA:", err)
	} else {
		l.Println("CA: Conexão estabelecida.")
	}

	return Sessions{
		DB:     postgresSession,
		CASDM:  caSession,
		Zabbix: zabbixSession,
	}
}

func main() {
	caURL := "http://x.x.x.x:8050/caisd-rest"
	zabbixURL := "http://x.x.x.x"
	postgresURL := "postgres://postgres:123456@localhost?sslmode=disable"

	l := log.New(os.Stdout, "", log.LstdFlags)

	s := NewSessions(caURL, zabbixURL, postgresURL, l)

	// A cada n segundos verificar que triggers existem no Zabbix.
	go func(n int) {
		for range time.Tick(time.Duration(n) * time.Second) {
			// Verificar quais triggers ativos existem no Zabbix.
			if triggers, err := s.Zabbix.GetTriggers(zabbix.TriggerGetParams{
				ActiveOnly:  true,
				SelectHosts: zabbix.SelectExtendedOutput,
			}); err == nil {
				for _, t := range triggers {
					for _, h := range t.Hosts {
						// E inserir-los na database.
						ins := `INSERT INTO
							          triggers(host_id, host_name, trigger_id, trigger_description, send_to_ca)
							          VALUES($1, $2, $3, $4, $5)
							      RETURNING id;`
						s.DB.Exec(ins, // Os valores serão inseridos na database como:
							h.HostID,      // host_id             integer   not null,
							h.Hostname,    // host_name           char(30)  not null,
							t.TriggerID,   // trigger_id          integer   not null unique,
							t.Description, // trigger_description char(100) not null,
							false)         // send_to_ca          bool      not null
						// O send_to_ca é inicializado como falso para que o
						// usuário escolha quais eventos ele deseja que sejam enviados ao CA,
						// pela interface gráfica.
					}
				}
			}
		}
	}(5)

	//  Remover os hosts da database que não estão mais no Zabbix.
	go s.CleanUpHosts(l)

	// A cada n segundos, pesquisar na database os triggers que devem ser enviados ao CA.
	go func(n int64) {
		for range time.Tick(time.Duration(n) * time.Second) {
			// Selecionar somente os triggers que devem ser enviados ao CA.
			rows, err := s.DB.Query("SELECT trigger_id FROM triggers WHERE send_to_ca='t'")
			if err != nil {
				log.Println(err)
			}

			// Criar tipos para o Scan.
			var triggerIDs []string
			var triggerID int
			for rows.Next() {
				err := rows.Scan(&triggerID)
				if err != nil {
					log.Println(err)
				}
				// Criar lista de triggers a serem verificados no Zabbix...
				triggerIDs = append(triggerIDs, strconv.Itoa(triggerID))
			}

			// Executar somente se a lista de triggers for maior que zero.
			// Se for zero, o Zabbix retorna todos os incidentes.
			if len(triggerIDs) > 0 {
				// Marcar o tempo atual.
				now := time.Now().Unix()
				// Verificar os eventos que ocorreram no Zabbix...
				if events, err := s.Zabbix.GetEvents(zabbix.EventGetParams{
					SelectHosts: zabbix.SelectExtendedOutput,
					MinTime:     now - n*240, // de n*60 segundos atrás...
					MaxTime:     now,         // até agora.
					ObjectIDs:   triggerIDs,  // Utilizando a lista criada anteriormente, dos triggers que devem ser enviados ao CA.
				}); err == nil {
					for _, event := range events {
						var lastInsertId int
						// Tentar inserir evento na database.
						err := s.DB.QueryRow("INSERT INTO events(zabbix_id) VALUES($1) RETURNING id;", event.EventID).Scan(&lastInsertId)
						// Se não for um evento duplicado...
						if err == nil {
							// Criar um link para o evento:
							link := zabbixURL + "/tr_events.php" + "?triggerid=" + strconv.Itoa(event.ObjectID) + "&eventid=" + event.EventID
							// Criar um Incidente no CA.
							in, err := s.CASDM.Create("Integração Zabbix",
								"Evento de ID no Zabbix: "+event.EventID+" no host: "+event.Hosts[0].Hostname,
								"Link do evento: "+link)
							if err != nil {
								log.Println(err)
							} else {
								log.Println("CA: Incidente aberto: Número do incidente:", in.Number,
									"Link do evento no Zabbix:", link)
								// Atualizar a linha do evento na database com o número do incidente no CA.
								_, err := s.DB.Exec("UPDATE events SET ca_id=$1, ca_status=$2 WHERE zabbix_id=$3",
									in.Number, in.Status, event.EventID)
								if err != nil {
									log.Println(err)
								}
							}
						}
					}
				}
			}
		}
	}(5)

	// Rotas http.
	// Os handlers se encontram no arquivo handlers.go.
	http.Handle("/", handlers.ListEvents(s.DB))
	http.Handle("/list-events", handlers.ListEvents(s.DB))
	http.Handle("/list-triggers", handlers.ListTriggers(s.DB))
	http.Handle("/post-triggers", handlers.PostTriggers(s.DB))

	http.Handle("/css/",
		http.StripPrefix("/css/", http.FileServer(http.Dir("./css"))),
	)

	addr := "0.0.0.0:8080"
	log.Println("Frontend: Iniciando...")
	log.Println("Frontend: Ouvindo no endereço: " + addr)

	// Iniciar o servidor frontend.
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Println(err)
	}
	log.Fatal("Frontend terminado.")
}
