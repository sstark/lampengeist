package schedules

import "strings"

// Placing a lamp in a routine category uses three kinds of evidence, in
// descending order of how much they can be trusted:
//
//  1. The archetype the Hue app stores with every room and zone. It is an
//     enum picked from a list of icons, so it is the same whatever language
//     the user named their rooms in.
//  2. The room or zone name, matched against the vocabulary below.
//  3. The lamp's own name, which catches a "Schreibtischlampe" sitting in a
//     room nobody bothered to name properly.

// Hints is everything the bridge knows that can place one lamp in a routine
// category. Fields are listed in the order they are trusted.
type Hints struct {
	RoomArchetype  string
	RoomName       string
	ZoneArchetypes []string
	ZoneNames      []string
	LampName       string
}

// Classify picks a routine category for one lamp, falling through the
// available evidence until something matches.
func Classify(hints Hints) RoomKind {
	if kind, ok := ClassifyArchetype(hints.RoomArchetype); ok {
		return kind
	}
	if kind, ok := ClassifyName(hints.RoomName); ok {
		return kind
	}

	// Zones are an overlay on top of rooms. People who leave their rooms
	// named by floor ("Erdgeschoss") often do name their zones by purpose.
	for _, archetype := range hints.ZoneArchetypes {
		if kind, ok := ClassifyArchetype(archetype); ok {
			return kind
		}
	}
	for _, name := range hints.ZoneNames {
		if kind, ok := ClassifyName(name); ok {
			return kind
		}
	}

	if kind, ok := ClassifyName(hints.LampName); ok {
		return kind
	}
	return Other
}

// ClassifyRoom maps a room or zone name to a routine category, falling back
// to Other when the name says nothing useful.
func ClassifyRoom(name string) RoomKind {
	kind, _ := ClassifyName(name)
	return kind
}

// ClassifyArchetype maps a Hue room archetype to a routine category. The
// archetypes that say nothing about what a room is used for — "other",
// "home" and the ones naming a floor — are deliberately absent, so that the
// name gets a chance instead.
func ClassifyArchetype(archetype string) (RoomKind, bool) {
	kind, ok := archetypeKinds[strings.ToLower(strings.TrimSpace(archetype))]
	return kind, ok
}

var archetypeKinds = map[string]RoomKind{
	"balcony":      Outdoor,
	"barbecue":     Outdoor,
	"carport":      Outdoor,
	"driveway":     Outdoor,
	"front_door":   Outdoor,
	"garage":       Outdoor,
	"garden":       Outdoor,
	"pool":         Outdoor,
	"porch":        Outdoor,
	"terrace":      Outdoor,
	"bathroom":     Bathroom,
	"toilet":       Bathroom,
	"kitchen":      Kitchen,
	"dining":       Kitchen,
	"bedroom":      Bedroom,
	"guest_room":   Bedroom,
	"kids_bedroom": Bedroom,
	"nursery":      Bedroom,
	"office":       Office,
	"computer":     Office,
	"studio":       Office,
	"hallway":      Hallway,
	"staircase":    Hallway,
	"living_room":  Living,
	"lounge":       Living,
	"man_cave":     Living,
	"music":        Living,
	"reading":      Living,
	"recreation":   Living,
	"tv":           Living,
	"attic":        Other,
	"closet":       Other,
	"gym":          Other,
	"laundry_room": Other,
	"storage":      Other,
}

// ClassifyName matches a room, zone or lamp name against the vocabulary,
// reporting whether anything matched at all. Callers that have more evidence
// to try use that; ClassifyRoom just takes Other.
func ClassifyName(name string) (RoomKind, bool) {
	folded := foldName(name)
	if folded == "" {
		return Other, false
	}

	tokens := strings.FieldsFunc(folded, func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	})

	for _, pattern := range roomPatterns {
		for _, needle := range pattern.contains {
			if strings.Contains(folded, needle) {
				return pattern.kind, true
			}
		}
		for _, word := range pattern.words {
			for _, token := range tokens {
				if token == word {
					return pattern.kind, true
				}
			}
		}
	}
	return Other, false
}

// roomPattern matches names against one routine category.
//
// contains matches anywhere in the folded name. That is what makes the
// compounding languages work: German, Dutch and the Nordic ones glue the
// room onto the lamp ("Schlafzimmerlampe", "keukenlamp", "sovrumslampa").
//
// words matches whole tokens only. Short or ambiguous terms go here, because
// as substrings they fire inside unrelated words: "hal" inside "halogen",
// "tv" inside "tvättstuga", "have" inside "Haven".
type roomPattern struct {
	kind     RoomKind
	contains []string
	words    []string
}

// roomPatterns is evaluated in order and the first match wins, so the
// entries are arranged to resolve the overlaps between them. Keywords are
// written in the folded form produced by foldName: lower case, accents
// reduced to plain letters.
var roomPatterns = []roomPattern{
	{
		// Utility rooms are matched first, and only for the names that
		// would otherwise be captured by a later category: "Waschküche"
		// ends in "küche", "tvättstuga" starts with "tv". Everything else
		// unrecognised reaches Other anyway.
		kind: Other,
		contains: []string{
			"waschkuche", "waschkueche", // de
			"bijkeuken",                  // nl, scullery
			"tvatt",                      // sv, laundry
			"spisekammer", "spiskammare", // da/no, sv, pantry
			"kuchynska linka", // cs, kitchen unit
		},
	},
	{
		kind: Outdoor,
		contains: []string{
			// en
			"outdoor", "garden", "terrace", "patio", "porch", "balcony", "driveway",
			"garage", "carport", "shed", "veranda", "backyard", "front door", "pool",
			// de
			"garten", "terrasse", "balkon", "aussen", "draussen", "hinterhof",
			"vorgarten", "einfahrt", "schuppen", "gartenhaus", "laube",
			// fr
			"jardin", "terrasse", "balcon", "exterieur", "allee de garage",
			"piscine", "abri de jardin", "porche",
			// nl
			"tuin", "terras", "oprit", "veranda", "schuur", "buiten", "voordeur",
			// pl
			"ogrod", "taras", "balkon", "podjazd", "garaz", "basen", "szopa", "dwor",
			// cs
			"zahrada", "terasa", "balkon", "prijezd", "bazen", "kulna", "venkovni",
			// it
			"giardino", "terrazza", "terrazzo", "balcone", "cortile", "piscina",
			"autorimessa", "esterno", "portico",
			// es
			"jardin", "terraza", "balcon", "patio", "piscina", "garaje", "cochera",
			"exterior", "porche",
			// pt
			"jardim", "terraco", "varanda", "quintal", "piscina", "garagem", "alpendre",
			// no/da/sv
			"hage", "uteplass", "uteplats", "terrasse", "terrass", "balkong", "altan",
			"tradgard", "garasje", "carport", "utendors", "udendors", "utomhus",
		},
		words: []string{"hof", "have", "cour", "ute", "ude"},
	},
	{
		// Before Bedroom, because Portuguese calls a bathroom a "quarto de
		// banho" and "quarto" on its own is a bedroom.
		kind: Bathroom,
		contains: []string{
			// en
			"bathroom", "shower", "toilet", "restroom", "washroom", "ensuite",
			// de
			"badezimmer", "badzimmer", "dusche", "gaste wc", "gastetoilette",
			"gaestetoilette", "waschbecken",
			// fr
			"salle de bain", "salle d'eau", "salle de douche", "toilettes", "lavabo",
			// nl
			"badkamer", "douche", "toilet", "wasbak",
			// pl
			"lazienka", "prysznic", "toaleta", "umywalka",
			// cs
			"koupelna", "sprcha", "zachod", "umyvadlo",
			// it
			"bagno", "doccia", "servizi igienici",
			// es
			"bano", "banyo", "ducha", "lavabo",
			// pt
			"banho", "banheiro", "duche", "chuveiro", "lavabo",
			// no/da/sv
			"baderom", "badevaerelse", "badrum", "toalett", "dusj", "dusch",
			// "bad" compounds freely in German and the Nordic languages
			// ("Kinderbad", "Badlampe", "badstue"), so it is a substring here.
			"bad",
		},
		words: []string{"wc", "spa", "aseo", "sauna", "bastu"},
	},
	{
		kind: Kitchen,
		contains: []string{
			// en
			"kitchen", "dining", "breakfast",
			// de
			"kueche", "kuche", "esszimmer", "essbereich", "esstisch", "speisezimmer",
			// fr
			"cuisine", "salle a manger", "coin repas",
			// nl
			"keuken", "eetkamer", "eethoek",
			// pl
			"kuchnia", "jadalnia",
			// cs
			"kuchyne", "kuchyn", "jidelna",
			// it
			"cucina", "sala da pranzo", "pranzo",
			// es
			"cocina", "comedor",
			// pt
			"cozinha", "sala de jantar", "jantar", "copa",
			// no/da/sv
			"kjokken", "kokken", "spisestue", "spisekrok", "matsal", "matplats",
		},
		words: []string{"kok"},
	},
	{
		kind: Bedroom,
		contains: []string{
			// en
			"bedroom", "nursery", "guest room", "kids", "child", "nightstand", "bedside",
			// de
			"schlafzimmer", "schlafraum", "kinderzimmer", "gastezimmer", "gaestezimmer",
			"nachttisch", "babyzimmer", "jugendzimmer",
			// fr
			"chambre", "table de nuit", "chevet",
			// nl
			"slaapkamer", "kinderkamer", "logeerkamer", "nachtkastje", "babykamer",
			// pl
			"sypialnia", "pokoj dziecie", "pokoj goscinny", "szafka nocna",
			// cs
			"loznice", "detsky pokoj", "pokoj pro hosty", "nocni stolek",
			// it
			"camera da letto", "cameretta", "stanza da letto", "comodino",
			// es
			"dormitorio", "habitacion", "recamara", "alcoba", "mesita de noche",
			// pt
			"quarto", "mesa de cabeceira", "cabeceira",
			// no/da/sv
			"soverom", "sovevaerelse", "sovevarelse", "sovrum", "gjesterom",
			"gaesterum", "gastrum", "barnerom", "barnrum", "nattbord", "sangbord",
		},
		words: []string{"camera", "bed", "seng"},
	},
	{
		kind: Office,
		contains: []string{
			// en
			"office", "study", "workshop", "workroom", "workspace", "desk", "homeoffice",
			// de
			"arbeitszimmer", "arbeitsraum", "arbeitsplatz", "buero", "buro",
			"schreibtisch", "werkstatt", "atelier", "studierzimmer", "werkraum",
			// fr
			"bureau", "cabinet de travail", "espace de travail", "atelier",
			// nl
			"kantoor", "werkkamer", "studeerkamer", "werkplek", "bureau",
			// pl
			"biuro", "gabinet", "pracownia", "biurko",
			// cs
			"pracovna", "kancelar", "psaci stul", "dilna",
			// it
			"ufficio", "studio", "scrivania", "laboratorio",
			// es
			"oficina", "despacho", "estudio", "escritorio",
			// pt
			"escritorio", "gabinete", "secretaria",
			// no/da/sv
			"kontor", "arbeidsrom", "arbejdsvaerelse", "arbetsrum", "skrivebord",
			"skrivbord", "verksted", "verkstad",
		},
	},
	{
		kind: Hallway,
		contains: []string{
			// en
			"hallway", "corridor", "landing", "staircase", "stairs", "stairway",
			"entrance", "entryway", "foyer", "porch light",
			// de
			"flur", "diele", "treppe", "treppenhaus", "eingang", "vorraum", "windfang",
			// fr
			"couloir", "entree", "vestibule", "escalier", "palier", "degagement",
			// nl
			"overloop", "portaal", "trap", "entree",
			// pl
			"korytarz", "przedpokoj", "schody", "klatka", "wejscie",
			// cs
			"chodba", "predsin", "schodiste", "schody", "vstup",
			// it
			"corridoio", "ingresso", "scala", "atrio", "pianerottolo",
			// es
			"pasillo", "recibidor", "escalera", "vestibulo", "zaguan",
			// pt
			"corredor", "entrada", "escada", "vestibulo", "patamar",
			// no/da/sv
			"gangen", "trapp", "trappe", "trappa", "farstu", "vindfang",
			"hallen", "forstue",
		},
		words: []string{"hal", "hall", "hol", "gang", "entre", "entry"},
	},
	{
		kind: Living,
		contains: []string{
			// en
			"living", "lounge", "sitting room", "family room", "reading",
			"library", "parlour", "parlor", "sofa", "couch", "fireplace",
			// de
			"wohnzimmer", "wohnraum", "wohnbereich", "stube", "lesezimmer",
			"bibliothek", "kamin", "fernseh", "sofa", "couch", "salon",
			// fr
			"salon", "sejour", "salle de sejour", "piece a vivre", "canape",
			"bibliotheque", "cheminee",
			// nl
			"woonkamer", "huiskamer", "zitkamer", "woonruimte", "leeshoek",
			"bibliotheek", "haard",
			// pl
			"salon", "pokoj dzienny", "kanapa", "biblioteka", "kominek",
			// cs
			"obyvaci pokoj", "obyvak", "obyvaci", "pohovka", "knihovna",
			// it
			"soggiorno", "salotto", "sala da pranzo e soggiorno", "divano",
			"biblioteca", "caminetto",
			// es
			"salon", "sala de estar", "estancia", "sofa", "biblioteca", "chimenea",
			// pt
			"sala de estar", "sala comum", "sofa", "biblioteca", "lareira",
			// no/da/sv
			"stue", "oppholdsrom", "vardagsrum", "allrum", "dagligstue", "sofa",
			"peis", "bibliotek",
		},
		words: []string{"tv", "den", "krb", "sala", "salong"},
	},
}

// foldName lower-cases a name and reduces the accented letters of the
// supported languages to plain ASCII, so "Küche", "KUECHE" and "kuche" all
// reach the same keyword. Keywords are written in this folded form.
//
// German, Danish and Norwegian users often spell the accents out instead of
// typing them, so the digraphs stay distinct ("ü" folds to "u", and "kueche"
// is listed as its own keyword) rather than being guessed at here.
func foldName(name string) string {
	var folded strings.Builder
	folded.Grow(len(name))

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if replacement, ok := foldedRunes[r]; ok {
			folded.WriteString(replacement)
			continue
		}
		folded.WriteRune(r)
	}
	return folded.String()
}

var foldedRunes = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'č': "c",
	'ď': "d", 'đ': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ę': "e", 'ě': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i",
	'ł': "l", 'ĺ': "l", 'ľ': "l",
	'ñ': "n", 'ń': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ő': "o",
	'ř': "r", 'ŕ': "r",
	'ś': "s", 'š': "s", 'ş': "s",
	'ť': "t", 'ţ': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ů': "u", 'ű': "u",
	'ý': "y", 'ÿ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
	'ß': "ss", 'æ': "ae", 'œ': "oe",
}
