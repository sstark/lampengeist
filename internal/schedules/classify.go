package schedules

import (
	"strings"
	"unicode"
)

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

	tokens := tokenize(folded)

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

// tokenize splits a folded name into the whole words that roomPattern.words
// is matched against.
//
// Marks count as part of a word rather than as separators: the vowel signs
// that carry half of a Devanagari word are combining marks, so splitting on
// them would reduce "सोने का कमरा" to its bare consonants. Scripts written
// without spaces at all — Chinese and Japanese — produce one token per run of
// script, which is why their vocabulary lives in contains.
func tokenize(folded string) []string {
	return strings.FieldsFunc(folded, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
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
			// zh: 家庭 is "family", and turns up in names like 家庭影院
			// (home cinema). It has to be caught before 庭 reaches Outdoor
			// as a garden. Other is no loss here: the routines treat it like
			// a living room, which is what a family room is.
			"家庭",
			// ro: "camera de zi" is a living room, but "camera" on its own
			// is an Italian bedroom and would claim it two patterns earlier.
			// Landing in Other gives it living-room timing, which is the
			// point; only the label in the UI is poorer.
			"camera de zi",
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
			// ru
			"терраса", "балкон", "лоджия", "гараж", "бассейн", "веранда",
			"беседка", "крыльцо", "двор", "садов", "огород", "навес",
			"газон", "уличн",
			// zh (simplified, then traditional)
			"花园", "阳台", "露台", "庭院", "车库", "车棚", "游泳池", "泳池",
			"户外", "门廊", "后院", "草坪",
			"花園", "陽台", "車庫", "車棚", "遊泳池", "戶外", "門廊", "後院",
			// ja. 玄関前 ("in front of the entrance") comes before Hallway's
			// 玄関 because Outdoor is matched first.
			"庭", "庭園", "バルコニー", "ベランダ", "テラス", "ガレージ", "車庫",
			"プール", "屋外", "裏庭", "玄関前", "ポーチ",
			// hi
			"बगीचा", "बालकनी", "आंगन", "गैराज", "बरामदा", "लॉन",
			// ur
			"باغ", "بالکونی", "صحن", "گیراج", "برامدہ", "چھت",
			// ar
			"حديقة", "شرفة", "بلكونة", "تراس", "فناء", "جراج", "كراج",
			"مراب", "مسبح", "خارجي",
			// vi
			"san vuon", "vuon", "ban cong", "san thuong", "nha xe", "ho boi",
			"be boi", "ngoai troi", "gara",
			// th
			"สวน", "ระเบียง", "เฉลียง", "ลานบ้าน", "โรงรถ", "สระว่ายน้ำ",
			"กลางแจ้ง", "นอกบ้าน",
			// sw
			"bustani", "roshani", "baraza", "uwanja", "gereji", "bwawa",
			// ro
			"gradina", "curte", "foisor",
			// tr
			// "teras" has one r, unlike the Dutch and Swedish forms above;
			// it covers the Romanian and Czech "terasa" too.
			"bahce", "havuz", "avlu", "disari", "garaj", "teras",
		},
		words: []string{
			"hof", "have", "cour", "ute", "ude",
			"сад", "сада", "саду", // ru
			"छत",   // hi, a flat roof used as a terrace
			"سطح",  // ar, roof
			"hien", // vi, a porch
			"nje",  // sw, outside
		},
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
			// ru. The bathroom words are whole tokens below, not substrings:
			// "душ" hides inside "подушка" (a pillow) and "ванная" inside
			// "диванная" (a sofa room).
			"туалет", "санузел", "уборная", "душевая", "умывальник", "унитаз",
			// zh
			"浴室", "卫生间", "洗手间", "厕所", "淋浴", "马桶",
			"衛生間", "洗手間", "廁所", "馬桶",
			// ja
			"風呂", "バスルーム", "トイレ", "洗面所", "シャワー", "手洗い",
			// hi
			"स्नानघर", "स्नान घर", "बाथरूम", "शौचालय", "गुसलखाना", "टॉयलेट",
			// ur
			"غسل خانہ", "غسلخانہ", "باتھ روم", "ٹوائلٹ", "بیت الخلا",
			// ar
			"حمام", "دورة المياه", "مرحاض", "بيت الخلاء",
			// vi
			"phong tam", "nha tam", "nha ve sinh", "voi sen",
			// th
			"ห้องน้ำ", "ห้องอาบน้ำ", "สุขา", "ส้วม", "ฝักบัว",
			// sw
			"bafu", "choo", "msala", "chumba cha kuoga",
			// ro
			"grup sanitar", "lavoar",
			// tr
			"tuvalet",
		},
		words: []string{
			"wc", "spa", "aseo", "sauna", "bastu",
			"ванная", "ванна", "ванной", "ванную", "душ", "сауна", // ru
			"دوش",                 // ar/ur, shower
			"baie", "baia", "dus", // ro; "dus" is also a Turkish shower
		},
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
			// ru
			"кухн", "кухон", "столовая", "обеденн",
			// zh
			"厨房", "餐厅", "饭厅", "餐桌", "餐区",
			"廚房", "餐廳", "飯廳",
			// ja. リビングダイニング, the combined living-dining room that
			// Japanese flats are usually described by, lands here rather
			// than in Living, the same way "sala da pranzo e soggiorno"
			// does.
			"キッチン", "台所", "厨房", "ダイニング", "食堂",
			// hi
			"रसोई", "किचन", "भोजन कक्ष", "भोजनकक्ष", "खाने का कमरा",
			// ur
			"باورچی خانہ", "باورچیخانہ", "کچن", "کھانے کا کمرہ",
			// ar
			"مطبخ", "غرفة الطعام", "صالة الطعام", "غرفة السفرة",
			// vi
			"nha bep", "bep", "phong an",
			// th
			"ห้องครัว", "ครัว", "ห้องอาหาร", "โต๊ะอาหาร",
			// sw
			"jiko", "chumba cha kulia", "meza ya kulia",
			// ro
			"bucatarie", "sufragerie", "sala de mese",
			// tr
			"mutfak", "yemek odasi",
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
			// ru. "гостевая" (guest) before Living's "гостиная" (sitting).
			"спальн", "детск", "гостевая", "кроват", "ночной столик",
			// zh
			"卧室", "儿童房", "客房", "婴儿房", "床头", "床边",
			"臥室", "兒童房", "嬰兒房", "床頭", "床邊",
			// ja. 客間, a guest room, before Living's 居間.
			"寝室", "ベッドルーム", "子供部屋", "子ども部屋", "客間", "枕元", "ベッド",
			// hi
			"शयनकक्ष", "शयन कक्ष", "बेडरूम", "सोने का कमरा", "बच्चों का कमरा",
			// ur
			"سونے کا کمرہ", "بیڈ روم", "خواب گاہ", "خوابگاہ", "بچوں کا کمرہ",
			// ar
			"غرفة النوم", "غرفة الاطفال", "غرفة الضيوف", "سرير",
			// vi. "phong khach" looks like a guest room but is the living
			// room; a Vietnamese guest bedroom is "phong ngu cho khach",
			// which still matches "phong ngu" here.
			"phong ngu", "giuong", "phong tre", "den ngu",
			// th
			"ห้องนอน", "เตียง", "ห้องเด็ก",
			// sw
			"chumba cha kulala", "kitanda", "chumba cha wageni",
			"chumba cha watoto",
			// ro
			"dormitor", "noptiera", "camera de oaspeti",
			// tr
			"yatak", "cocuk odasi", "bebek odasi",
		},
		words: []string{
			"camera", "bed", "seng",
			"نوم", // ar
			"pat", // ro, a bed; "patio" is matched as Outdoor well before this
		},
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
			// ru
			"кабинет", "офис", "мастерская", "письменный стол", "рабочий стол",
			"рабочая комната", "верстак",
			// zh
			"书房", "办公室", "工作室", "工作间", "书桌", "车间",
			"書房", "辦公室", "書桌", "車間",
			// ja. 机 ("desk") is left out: as a substring it would fire on
			// Simplified Chinese 机 ("machine"), as in 洗衣机, a washing
			// machine.
			"書斎", "仕事部屋", "作業部屋", "オフィス", "デスク", "工房",
			// hi
			"कार्यालय", "दफ्तर", "ऑफिस", "अध्ययन कक्ष", "स्टडी",
			// ur
			"دفتر", "مطالعہ", "افس", "کام کا کمرہ",
			// ar
			"مكتب", "ورشة", "غرفة الدراسة",
			// vi
			// "đèn bàn" is a desk lamp; "đèn ban công" reaches Outdoor first.
			"phong lam viec", "van phong", "ban lam viec", "den ban",
			// th
			"ทำงาน", "สำนักงาน", "ออฟฟิศ", "ห้องหนังสือ",
			// sw. "karakana" is a workshop; a Swahili garage is "gereji".
			"ofisi", "chumba cha kazi", "dawati", "karakana",
			// ro
			"birou", "masa de lucru",
			// tr
			"ofis", "calisma", "atolye",
		},
		words: []string{
			"मेज", // hi, a table or desk
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
			// ru
			"коридор", "прихожая", "лестниц", "тамбур", "вестибюль",
			// zh
			"走廊", "玄关", "门厅", "楼梯", "过道", "入口",
			"玄關", "門廳", "樓梯", "過道",
			// ja
			"廊下", "玄関", "階段", "通路", "エントランス", "ホール",
			// hi
			"गलियारा", "प्रवेश", "सीढ", "दालान",
			// ur
			"راہداری", "سیڑھیاں", "سیڑھی", "دالان",
			// ar
			"ممر", "مدخل", "ردهة", "بهو",
			// vi
			"hanh lang", "cau thang", "loi vao",
			// th
			"ทางเดิน", "บันได", "ทางเข้า",
			// sw. "mlango" is a door, "lango" a gate; both say entrance.
			"ukumbi", "korido", "ngazi", "lango", "njia ya kuingia",
			// ro
			"coridor", "intrare", "vestibul",
			// tr
			"koridor", "merdiven", "giris", "sahanlik",
		},
		words: []string{
			"hal", "hall", "hol", "gang", "entre", "entry",
			"холл", "вход", // ru
			"درج", "سلم", // ar, stairs; both are common words on their own
			"sanh",           // vi, a lobby
			"scara", "scari", // ro, stairs
			"antre", // tr, a hall
		},
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
			// ru
			"гостиная", "диван", "камин", "библиотек", "телевизор", "кресло",
			// zh
			"客厅", "起居室", "沙发", "壁炉", "电视",
			"客廳", "沙發", "壁爐", "電視",
			// ja
			"リビング", "居間", "応接間", "ソファ", "暖炉", "テレビ",
			// hi
			"बैठक", "लिविंग रूम", "ड्राइंग रूम", "सोफा",
			// ur
			"بیٹھک", "ڈرائنگ روم", "کمرہ نشست", "صوفہ",
			// ar
			"غرفة الجلوس", "الجلوس", "صالون", "مجلس", "معيشة", "اريكة", "صالة",
			// vi
			"phong khach", "phong sinh hoat", "lo suoi", "tivi",
			// th
			"ห้องนั่งเล่น", "ห้องโถง", "โซฟา", "ทีวี", "เตาผิง",
			// sw. "maktaba" is a library, which English puts here too.
			"sebule", "chumba cha kukaa", "kochi", "maktaba",
			// ro
			"semineu",
			// tr. "misafir odasi" is the formal sitting room kept for
			// visitors, not a guest bedroom.
			"oturma odasi", "misafir odasi", "kanepe", "somine", "kutuphane",
		},
		words: []string{
			// "den", the American word for a small living room, used to be
			// here. It had to go: it folds together with Vietnamese "đèn",
			// which is simply the word for "lamp", so it put every lamp in a
			// Vietnamese home in the living room. English has "living room",
			// "lounge", "sitting room" and "family room" above to fall back
			// on; "đèn" has no alternative spelling.
			"tv", "krb", "sala", "salong",
			"зал", "зала", // ru
		},
	},
}

// foldName lower-cases a name and reduces the accented letters of the
// supported languages to plain ASCII, so "Küche", "KUECHE" and "kuche" all
// reach the same keyword. Keywords are written in this folded form.
//
// German, Danish and Norwegian users often spell the accents out instead of
// typing them, so the digraphs stay distinct ("ü" folds to "u", and "kueche"
// is listed as its own keyword) rather than being guessed at here.
//
// The non-Latin scripts are folded only where a single word has more than one
// accepted encoding, which is why the keywords below look unpointed:
//
//   - Arabic drops the vowel marks and the tatweel stretching character, and
//     the alef variants collapse to plain alef. "غرفة" and "غُرْفَة" are the same
//     word typed with and without the pointing, and "الأطفال" is as often
//     typed "الاطفال".
//   - Devanagari drops the nukta, so "दफ़्तर" and "दफ्तर" meet, and the
//     candrabindu folds to the anusvara it is interchangeable with in
//     "आँगन"/"आंगन".
//
// Urdu is left on its own letters (ک, ی, ہ) rather than folded onto the
// Arabic ones it resembles, because that is what an Urdu keyboard produces;
// the few words shared with Arabic are simply listed in both spellings.
// Chinese is not folded at all — Simplified and Traditional are listed as the
// separate vocabularies they are — and Japanese needs no folding, as the
// script a room name is written in is a real choice there, not a variant.
// Thai is likewise untouched: its vowels and tone marks are combining marks
// that carry the word, so dropping them the way Arabic pointing is dropped
// would leave only the consonants.
func foldName(name string) string {
	var folded strings.Builder
	folded.Grow(len(name))

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if replacement, ok := foldedRunes[r]; ok {
			folded.WriteString(replacement)
			continue
		}
		if base, ok := foldedVietnamese(r); ok {
			folded.WriteString(base)
			continue
		}
		folded.WriteRune(r)
	}
	return folded.String()
}

// foldedVietnamese reduces the Latin Extended Additional block to plain
// letters. Vietnamese stacks a vowel mark and a tone mark on one letter, which
// precomposes to 67 lowercase codepoints — too many to list, and unnecessary:
// the block is laid out in one contiguous run per base vowel, so the base
// letter follows from the range alone. U+1EFA and up are Middle Welsh and
// nothing to do with Vietnamese, and are left alone.
func foldedVietnamese(r rune) (string, bool) {
	switch {
	case r >= 0x1EA0 && r <= 0x1EB7: // ạ ả ấ ầ ẩ ẫ ậ ắ ằ ẳ ẵ ặ
		return "a", true
	case r >= 0x1EB8 && r <= 0x1EC7: // ẹ ẻ ẽ ế ề ể ễ ệ
		return "e", true
	case r >= 0x1EC8 && r <= 0x1ECB: // ỉ ị
		return "i", true
	case r >= 0x1ECC && r <= 0x1EE3: // ọ ỏ ố ồ ổ ỗ ộ ớ ờ ở ỡ ợ
		return "o", true
	case r >= 0x1EE4 && r <= 0x1EF1: // ụ ủ ứ ừ ử ữ ự
		return "u", true
	case r >= 0x1EF2 && r <= 0x1EF9: // ỳ ỵ ỷ ỹ
		return "y", true
	}
	return "", false
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

	// vi: the letters outside the Latin Extended Additional block that
	// foldedVietnamese covers. ("đ" is already folded above.)
	'ă': "a", 'ĩ': "i", 'ũ': "u", 'ơ': "o", 'ư': "u",
	// tr: the dotless i, and the soft g. Go lower-cases "İ" to a plain "i",
	// so the dotted capital needs nothing of its own.
	'ı': "i", 'ğ': "g",
	// ro: the comma-below letters. Romanian is often typed with the Turkish
	// cedilla forms instead (ş, ţ), which are folded above.
	'ș': "s", 'ț': "t",

	// ru: ё is written е as often as not.
	'ё': "е",

	// ar: alef variants, and the vowel pointing and tatweel that carry no
	// meaning for a room name. Dropped runes map to the empty string.
	'أ': "ا", 'إ': "ا", 'آ': "ا", 'ٱ': "ا", 'ى': "ي",
	'ً': "", 'ٌ': "", 'ٍ': "", 'َ': "", // fathatan..fatha
	'ُ': "", 'ِ': "", 'ّ': "", 'ْ': "", // damma..sukun
	'ٓ': "", 'ٔ': "", 'ٕ': "", 'ٰ': "", // maddah..superscript alef
	'ـ': "", // tatweel

	// hi: the nukta, both standalone and in the precomposed letters, and the
	// candrabindu that stands in for the anusvara.
	'़': "",  // nukta
	'ँ': "ं", // candrabindu -> anusvara
	// The precomposed nukta letters (U+0958..U+095F). On screen they are
	// indistinguishable from the base letter followed by a nukta, which is
	// the encoding they fold onto, so check the codepoint and not the glyph
	// when editing these.
	'क़': "क", // क़
	'ख़': "ख", // ख़
	'ग़': "ग", // ग़
	'ज़': "ज", // ज़
	'ड़': "ड", // ड़
	'ढ़': "ढ", // ढ़
	'फ़': "फ", // फ़
	'य़': "य", // य़
}
