package schedules

import "testing"

// TestClassifyNamePerLanguage walks the vocabulary language by language. The
// point is coverage, not cleverness: every category has to be reachable from
// a name a native speaker would plausibly type into the Hue app.
func TestClassifyNamePerLanguage(t *testing.T) {
	languages := map[string]map[string]RoomKind{
		"english": {
			"Kitchen": Kitchen, "Dining room": Kitchen,
			"Bathroom": Bathroom, "Downstairs toilet": Bathroom, "Shower": Bathroom,
			"Bedroom": Bedroom, "Guest room": Bedroom, "Kids room": Bedroom, "Nursery": Bedroom,
			"Office": Office, "Study": Office, "Workshop": Office, "Desk lamp": Office,
			"Hallway": Hallway, "Corridor": Hallway, "Stairs": Hallway, "Entrance": Hallway,
			"Living room": Living, "Lounge": Living, "Sitting room": Living, "Library": Living,
			"Garden": Outdoor, "Terrace": Outdoor, "Balcony": Outdoor, "Driveway": Outdoor,
			"Carport": Outdoor, "Garage": Outdoor, "Patio": Outdoor,
		},
		"german": {
			"Küche": Kitchen, "Kueche": Kitchen, "Esszimmer": Kitchen, "Speisezimmer": Kitchen,
			"Badezimmer": Bathroom, "Bad": Bathroom, "Gäste WC": Bathroom, "Dusche": Bathroom,
			"Schlafzimmer": Bedroom, "Kinderzimmer": Bedroom, "Gästezimmer": Bedroom,
			"Gaestezimmer": Bedroom, "Nachttischlampe": Bedroom,
			"Arbeitszimmer": Office, "Büro": Office, "Buero": Office, "Werkstatt": Office,
			"Schreibtischlampe": Office, "Homeoffice": Office, "Atelier": Office,
			"Flur": Hallway, "Diele": Hallway, "Treppenhaus": Hallway, "Eingang": Hallway,
			"Wohnzimmer": Living, "Stube": Living, "Lesezimmer": Living, "Bibliothek": Living,
			"Garten": Outdoor, "Terrasse": Outdoor, "Balkon": Outdoor, "Carport": Outdoor,
			"Schuppen": Outdoor, "Einfahrt": Outdoor, "Vorgarten": Outdoor,
		},
		"french": {
			"Cuisine": Kitchen, "Salle à manger": Kitchen,
			"Salle de bain": Bathroom, "Salle de bains": Bathroom, "Toilettes": Bathroom,
			"Chambre": Bedroom, "Chambre d'amis": Bedroom, "Table de nuit": Bedroom,
			"Bureau": Office, "Cabinet de travail": Office,
			"Couloir": Hallway, "Entrée": Hallway, "Escalier": Hallway, "Vestibule": Hallway,
			"Salon": Living, "Séjour": Living, "Salle de séjour": Living, "Bibliothèque": Living,
			"Jardin": Outdoor, "Terrasse": Outdoor, "Balcon": Outdoor, "Piscine": Outdoor,
		},
		"dutch": {
			"Keuken": Kitchen, "Eetkamer": Kitchen,
			"Badkamer": Bathroom, "Toilet": Bathroom, "Douche": Bathroom,
			"Slaapkamer": Bedroom, "Kinderkamer": Bedroom, "Logeerkamer": Bedroom,
			"Kantoor": Office, "Werkkamer": Office, "Studeerkamer": Office,
			"Overloop": Hallway, "Gang": Hallway, "Hal": Hallway, "Trap": Hallway,
			"Woonkamer": Living, "Huiskamer": Living, "Zitkamer": Living,
			"Tuin": Outdoor, "Terras": Outdoor, "Oprit": Outdoor, "Schuur": Outdoor,
		},
		"polish": {
			"Kuchnia": Kitchen, "Jadalnia": Kitchen,
			"Łazienka": Bathroom, "Lazienka": Bathroom, "Toaleta": Bathroom,
			"Sypialnia": Bedroom, "Pokój dziecięcy": Bedroom, "Pokój gościnny": Bedroom,
			"Biuro": Office, "Gabinet": Office, "Pracownia": Office,
			"Korytarz": Hallway, "Przedpokój": Hallway, "Schody": Hallway,
			"Salon": Living, "Pokój dzienny": Living, "Biblioteka": Living,
			"Ogród": Outdoor, "Taras": Outdoor, "Balkon": Outdoor, "Garaż": Outdoor,
		},
		"czech": {
			"Kuchyně": Kitchen, "Kuchyn": Kitchen, "Jídelna": Kitchen,
			"Koupelna": Bathroom, "Záchod": Bathroom, "Sprcha": Bathroom,
			"Ložnice": Bedroom, "Dětský pokoj": Bedroom, "Pokoj pro hosty": Bedroom,
			"Pracovna": Office, "Kancelář": Office, "Dílna": Office,
			"Chodba": Hallway, "Předsíň": Hallway, "Schodiště": Hallway,
			"Obývací pokoj": Living, "Obývák": Living, "Knihovna": Living,
			"Zahrada": Outdoor, "Terasa": Outdoor, "Balkón": Outdoor, "Bazén": Outdoor,
		},
		"italian": {
			"Cucina": Kitchen, "Sala da pranzo": Kitchen,
			"Bagno": Bathroom, "Doccia": Bathroom,
			"Camera da letto": Bedroom, "Cameretta": Bedroom, "Comodino": Bedroom,
			"Ufficio": Office, "Studio": Office, "Scrivania": Office,
			"Corridoio": Hallway, "Ingresso": Hallway, "Scala": Hallway,
			"Soggiorno": Living, "Salotto": Living, "Biblioteca": Living,
			"Giardino": Outdoor, "Terrazza": Outdoor, "Balcone": Outdoor, "Cortile": Outdoor,
		},
		"spanish": {
			"Cocina": Kitchen, "Comedor": Kitchen,
			"Baño": Bathroom, "Bano": Bathroom, "Aseo": Bathroom, "Ducha": Bathroom,
			"Dormitorio": Bedroom, "Habitación": Bedroom, "Recámara": Bedroom, "Alcoba": Bedroom,
			"Oficina": Office, "Despacho": Office, "Escritorio": Office,
			"Pasillo": Hallway, "Recibidor": Hallway, "Escalera": Hallway, "Vestíbulo": Hallway,
			"Salón": Living, "Sala de estar": Living, "Sala": Living,
			"Jardín": Outdoor, "Terraza": Outdoor, "Balcón": Outdoor, "Garaje": Outdoor,
		},
		"portuguese": {
			"Cozinha": Kitchen, "Sala de jantar": Kitchen,
			"Casa de banho": Bathroom, "Banheiro": Bathroom, "Quarto de banho": Bathroom,
			"Quarto": Bedroom, "Mesa de cabeceira": Bedroom,
			"Escritório": Office, "Gabinete": Office,
			"Corredor": Hallway, "Entrada": Hallway, "Escada": Hallway,
			"Sala de estar": Living, "Sala comum": Living, "Biblioteca": Living,
			"Jardim": Outdoor, "Terraço": Outdoor, "Varanda": Outdoor, "Garagem": Outdoor,
		},
		"norwegian": {
			"Kjøkken": Kitchen, "Spisestue": Kitchen,
			"Bad": Bathroom, "Baderom": Bathroom, "Toalett": Bathroom, "Dusj": Bathroom,
			"Soverom": Bedroom, "Gjesterom": Bedroom, "Barnerom": Bedroom, "Nattbord": Bedroom,
			"Kontor": Office, "Arbeidsrom": Office, "Skrivebord": Office, "Verksted": Office,
			"Gangen": Hallway, "Entré": Hallway, "Trapp": Hallway,
			"Stue": Living, "Oppholdsrom": Living,
			"Hage": Outdoor, "Terrasse": Outdoor, "Balkong": Outdoor, "Uteplass": Outdoor,
			"Garasje": Outdoor,
		},
		"swedish": {
			"Kök": Kitchen, "Matsal": Kitchen, "Matplats": Kitchen,
			"Badrum": Bathroom, "Toalett": Bathroom, "Dusch": Bathroom,
			"Sovrum": Bedroom, "Barnrum": Bedroom, "Gästrum": Bedroom, "Sängbord": Bedroom,
			"Kontor": Office, "Arbetsrum": Office, "Skrivbord": Office, "Verkstad": Office,
			"Hall": Hallway, "Trappa": Hallway, "Farstu": Hallway,
			"Vardagsrum": Living, "Allrum": Living, "Bibliotek": Living,
			"Trädgård": Outdoor, "Terrass": Outdoor, "Balkong": Outdoor, "Altan": Outdoor,
			"Uteplats": Outdoor,
		},
		"danish": {
			"Køkken": Kitchen, "Spisestue": Kitchen,
			"Badeværelse": Bathroom, "Bad": Bathroom, "Toilet": Bathroom,
			"Soveværelse": Bedroom, "Sovevaerelse": Bedroom, "Gæsterum": Bedroom,
			"Kontor": Office, "Arbejdsværelse": Office, "Skrivebord": Office,
			"Entré": Hallway, "Trappe": Hallway, "Gang": Hallway,
			"Stue": Living, "Dagligstue": Living,
			"Have": Outdoor, "Terrasse": Outdoor, "Altan": Outdoor, "Garage": Outdoor,
		},
	}

	for language, names := range languages {
		t.Run(language, func(t *testing.T) {
			for name, want := range names {
				got, ok := ClassifyName(name)
				if !ok {
					t.Errorf("%q was not recognised at all, want %q", name, want)
					continue
				}
				if got != want {
					t.Errorf("ClassifyName(%q) = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestClassifyNameResolvesOverlaps pins the collisions that the ordering of
// roomPatterns exists to resolve. Each of these used to land, or would
// naturally land, in the wrong category.
func TestClassifyNameResolvesOverlaps(t *testing.T) {
	tests := map[string]RoomKind{
		"Waschküche":      Other,    // ends in "Küche"
		"Tvättstuga":      Other,    // starts with "tv", ends in a living-room word
		"Bijkeuken":       Other,    // contains "keuken"
		"Quarto de banho": Bathroom, // contains "quarto", which is a bedroom
		"Spisekammer":     Other,    // contains "spise", which is dining
		"Halogen":         Other,    // contains "hal", which is a hallway
		"Halogenlampe":    Other,
		"Sala de jantar":  Kitchen, // contains "sala", which is a living room
		"Badezimmer":      Bathroom,
		"Kinderbad":       Bathroom, // a child's bathroom is still a bathroom
		"Gästetoilette":   Bathroom,
		"Oppholdsrom":     Living, // contains "hol", a Polish hallway
		"Bodenlampe":      Other,  // contains "den", an English living room
		"Paseo":           Other,  // contains "aseo", a Spanish bathroom
		"Centre":          Other,  // contains "entre", a Nordic hallway
	}

	for name, want := range tests {
		if got := ClassifyRoom(name); got != want {
			t.Errorf("ClassifyRoom(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestClassifyNameAcceptedFalsePositives records where the vocabulary is
// knowingly wrong. "bad" has to match inside words, because German and the
// Nordic languages build half their bathroom names out of it ("Kinderbad",
// "Badlampe", "badstue"); the cost is the handful of unrelated words it also
// appears in. These are documented rather than fixed because no plausible
// Hue room is named after them — if one turns up, this is the test to change.
func TestClassifyNameAcceptedFalsePositives(t *testing.T) {
	tests := map[string]RoomKind{
		"Badminton": Bathroom,
		"Badge":     Bathroom,
	}

	for name, want := range tests {
		if got := ClassifyRoom(name); got != want {
			t.Errorf("ClassifyRoom(%q) = %q, want the documented %q", name, got, want)
		}
	}
}

func TestClassifyNameReportsUnknownNames(t *testing.T) {
	for _, name := range []string{"", "   ", "Zimmer 3", "Erdgeschoss", "Hue", "Lampe 1"} {
		if kind, ok := ClassifyName(name); ok {
			t.Errorf("ClassifyName(%q) claimed %q, want no match", name, kind)
		}
	}
}

func TestClassifyArchetype(t *testing.T) {
	tests := map[string]RoomKind{
		"office":       Office,
		"computer":     Office,
		"kitchen":      Kitchen,
		"dining":       Kitchen,
		"kids_bedroom": Bedroom,
		"front_door":   Outdoor,
		"staircase":    Hallway,
		"man_cave":     Living,
		"laundry_room": Other,
	}

	for archetype, want := range tests {
		got, ok := ClassifyArchetype(archetype)
		if !ok {
			t.Errorf("archetype %q was not recognised, want %q", archetype, want)
			continue
		}
		if got != want {
			t.Errorf("ClassifyArchetype(%q) = %q, want %q", archetype, got, want)
		}
	}

	// These say nothing about what a room is for, so the name must get a turn.
	for _, archetype := range []string{"other", "home", "upstairs", "downstairs", "top_floor", ""} {
		if kind, ok := ClassifyArchetype(archetype); ok {
			t.Errorf("archetype %q claimed %q, want it to defer to the name", archetype, kind)
		}
	}
}

// TestClassifyPrefersTheStrongerEvidence walks the fallback chain: the
// archetype is language independent and so beats a name, a name beats a
// zone, and the lamp's own name is the last resort.
func TestClassifyPrefersTheStrongerEvidence(t *testing.T) {
	tests := []struct {
		name  string
		hints Hints
		want  RoomKind
	}{
		{
			name:  "archetype wins over a misleading room name",
			hints: Hints{RoomArchetype: "office", RoomName: "Kitchen"},
			want:  Office,
		},
		{
			name:  "room name is used when the archetype says nothing",
			hints: Hints{RoomArchetype: "other", RoomName: "Arbeitszimmer"},
			want:  Office,
		},
		{
			name: "a zone names what the room does not",
			hints: Hints{
				RoomArchetype: "home",
				RoomName:      "Erdgeschoss",
				ZoneNames:     []string{"Arbeitszimmer"},
			},
			want: Office,
		},
		{
			name: "a zone archetype beats a zone name",
			hints: Hints{
				RoomName:       "Dachgeschoss",
				ZoneArchetypes: []string{"bedroom"},
				ZoneNames:      []string{"Arbeitszimmer"},
			},
			want: Bedroom,
		},
		{
			name: "the lamp name is the last resort",
			hints: Hints{
				RoomName: "Zimmer 2",
				LampName: "Schreibtischlampe",
			},
			want: Office,
		},
		{
			name:  "nothing to go on",
			hints: Hints{RoomName: "Zimmer 2", LampName: "Lampe 1"},
			want:  Other,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.hints); got != test.want {
				t.Fatalf("Classify(%+v) = %q, want %q", test.hints, got, test.want)
			}
		})
	}
}

func TestFoldName(t *testing.T) {
	tests := map[string]string{
		"Küche":        "kuche",
		"KÜCHE":        "kuche",
		"Łazienka":     "lazienka",
		"Soveværelse":  "sovevaerelse",
		"Trädgård":     "tradgard",
		"Baño":         "bano",
		"Obývací žito": "obyvaci zito",
		"Straße":       "strasse",
		"  Flur  ":     "flur",
	}

	for name, want := range tests {
		if got := foldName(name); got != want {
			t.Errorf("foldName(%q) = %q, want %q", name, got, want)
		}
	}
}
