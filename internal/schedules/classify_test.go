package schedules

import (
	"fmt"
	"strings"
	"testing"
)

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
		// Simplified and Traditional are both listed, because the bridge
		// stores whatever the user typed.
		"mandarin": {
			"厨房": Kitchen, "餐厅": Kitchen, "廚房": Kitchen, "餐廳": Kitchen, "餐桌": Kitchen,
			"浴室": Bathroom, "卫生间": Bathroom, "洗手间": Bathroom, "厕所": Bathroom,
			"衛生間": Bathroom, "廁所": Bathroom, "淋浴": Bathroom,
			"卧室": Bedroom, "儿童房": Bedroom, "客房": Bedroom, "婴儿房": Bedroom,
			"臥室": Bedroom, "兒童房": Bedroom, "床头灯": Bedroom,
			"书房": Office, "办公室": Office, "工作室": Office, "书桌": Office,
			"書房": Office, "辦公室": Office, "書桌": Office,
			"走廊": Hallway, "玄关": Hallway, "门厅": Hallway, "楼梯": Hallway,
			"过道": Hallway, "玄關": Hallway, "樓梯": Hallway,
			"客厅": Living, "起居室": Living, "沙发": Living, "电视": Living,
			"客廳": Living, "沙發": Living, "電視": Living,
			"花园": Outdoor, "阳台": Outdoor, "露台": Outdoor, "车库": Outdoor,
			"庭院": Outdoor, "后院": Outdoor, "花園": Outdoor, "陽台": Outdoor, "車庫": Outdoor,
		},
		"japanese": {
			"キッチン": Kitchen, "台所": Kitchen, "ダイニング": Kitchen, "食堂": Kitchen,
			"浴室": Bathroom, "お風呂": Bathroom, "風呂場": Bathroom, "トイレ": Bathroom,
			"洗面所": Bathroom, "シャワー": Bathroom, "バスルーム": Bathroom,
			"寝室": Bedroom, "子供部屋": Bedroom, "子ども部屋": Bedroom, "客間": Bedroom,
			"ベッドルーム": Bedroom, "枕元": Bedroom,
			"書斎": Office, "仕事部屋": Office, "オフィス": Office, "デスク": Office, "工房": Office,
			"廊下": Hallway, "玄関": Hallway, "階段": Hallway, "通路": Hallway,
			"エントランス": Hallway, "ホール": Hallway,
			"リビング": Living, "居間": Living, "応接間": Living, "ソファ": Living,
			"暖炉": Living, "テレビ": Living,
			"庭": Outdoor, "庭園": Outdoor, "バルコニー": Outdoor, "ベランダ": Outdoor,
			"テラス": Outdoor, "ガレージ": Outdoor, "プール": Outdoor, "裏庭": Outdoor,
		},
		"russian": {
			"Кухня": Kitchen, "Кухне": Kitchen, "Столовая": Kitchen, "Кухонный остров": Kitchen,
			"Ванная": Bathroom, "Ванная комната": Bathroom, "Туалет": Bathroom,
			"Душ": Bathroom, "Санузел": Bathroom, "Уборная": Bathroom,
			"Спальня": Bedroom, "Детская": Bedroom, "Детская комната": Bedroom,
			"Гостевая комната": Bedroom, "Кровать": Bedroom,
			"Кабинет": Office, "Офис": Office, "Мастерская": Office,
			"Письменный стол": Office, "Рабочий стол": Office,
			"Коридор": Hallway, "Прихожая": Hallway, "Лестница": Hallway, "Холл": Hallway,
			"Гостиная": Living, "Диван": Living, "Камин": Living,
			"Библиотека": Living, "Зал": Living, "Кресло": Living,
			"Сад": Outdoor, "Терраса": Outdoor, "Балкон": Outdoor, "Лоджия": Outdoor,
			"Гараж": Outdoor, "Двор": Outdoor, "Бассейн": Outdoor, "Веранда": Outdoor,
		},
		// Hindi in Devanagari, with the nukta both typed and left out.
		"hindi": {
			"रसोई": Kitchen, "रसोईघर": Kitchen, "किचन": Kitchen, "भोजन कक्ष": Kitchen,
			"स्नानघर": Bathroom, "बाथरूम": Bathroom, "शौचालय": Bathroom,
			"गुसलखाना": Bathroom, "टॉयलेट": Bathroom,
			"शयनकक्ष": Bedroom, "शयन कक्ष": Bedroom, "सोने का कमरा": Bedroom,
			"बेडरूम": Bedroom, "बच्चों का कमरा": Bedroom,
			"कार्यालय": Office, "दफ्तर": Office, "दफ़्तर": Office, "ऑफिस": Office,
			"अध्ययन कक्ष": Office, "मेज": Office,
			"गलियारा": Hallway, "प्रवेश": Hallway, "सीढ़ी": Hallway,
			"सीढ़ियाँ": Hallway, "दालान": Hallway,
			"बैठक": Living, "ड्राइंग रूम": Living, "लिविंग रूम": Living, "सोफा": Living,
			"बगीचा": Outdoor, "बग़ीचा": Outdoor, "बालकनी": Outdoor, "आंगन": Outdoor,
			"आँगन": Outdoor, "छत": Outdoor, "बरामदा": Outdoor, "गैराज": Outdoor,
		},
		// Urdu in its own letters: ک, ی and ہ, not the Arabic lookalikes.
		"urdu": {
			"باورچی خانہ": Kitchen, "کچن": Kitchen, "کھانے کا کمرہ": Kitchen,
			"غسل خانہ": Bathroom, "غسلخانہ": Bathroom, "باتھ روم": Bathroom, "ٹوائلٹ": Bathroom,
			"سونے کا کمرہ": Bedroom, "بیڈ روم": Bedroom, "خواب گاہ": Bedroom,
			"خوابگاہ": Bedroom, "بچوں کا کمرہ": Bedroom,
			"دفتر": Office, "مطالعہ": Office, "کام کا کمرہ": Office,
			"راہداری": Hallway, "سیڑھیاں": Hallway, "سیڑھی": Hallway, "دالان": Hallway,
			"بیٹھک": Living, "ڈرائنگ روم": Living, "کمرہ نشست": Living, "صوفہ": Living,
			"باغ": Outdoor, "بالکونی": Outdoor, "صحن": Outdoor, "چھت": Outdoor,
			"برآمدہ": Outdoor, "گیراج": Outdoor,
		},
		// Arabic with and without the vowel pointing and the alef variants.
		"arabic": {
			"مطبخ": Kitchen, "غرفة الطعام": Kitchen, "صالة الطعام": Kitchen,
			"حمام": Bathroom, "دورة المياه": Bathroom, "مرحاض": Bathroom, "دوش": Bathroom,
			"غرفة النوم": Bedroom, "غُرْفَة النوم": Bedroom, "غرفة الأطفال": Bedroom,
			"غرفة الاطفال": Bedroom, "غرفة الضيوف": Bedroom, "سرير": Bedroom,
			"مكتب": Office, "ورشة": Office, "غرفة الدراسة": Office,
			"ممر": Hallway, "مدخل": Hallway, "درج": Hallway, "ردهة": Hallway, "بهو": Hallway,
			"غرفة الجلوس": Living, "صالون": Living, "مجلس": Living,
			"صالة": Living, "أريكة": Living, "اريكة": Living,
			"حديقة": Outdoor, "شرفة": Outdoor, "فناء": Outdoor, "مسبح": Outdoor,
			"مرآب": Outdoor, "سطح": Outdoor, "تراس": Outdoor,
		},
		// Vietnamese stacks a vowel mark and a tone mark on one letter, so
		// every one of these arrives as precomposed codepoints.
		"vietnamese": {
			"Nhà bếp": Kitchen, "Phòng ăn": Kitchen,
			"Phòng tắm": Bathroom, "Nhà vệ sinh": Bathroom, "Vòi sen": Bathroom,
			"Phòng ngủ": Bedroom, "Giường": Bedroom, "Đèn ngủ": Bedroom,
			"Phòng làm việc": Office, "Văn phòng": Office, "Đèn bàn": Office,
			"Hành lang": Hallway, "Cầu thang": Hallway, "Lối vào": Hallway,
			"Sảnh":        Hallway,
			"Phòng khách": Living, "Phòng sinh hoạt": Living, "Lò sưởi": Living,
			"Sân vườn": Outdoor, "Ban công": Outdoor, "Sân thượng": Outdoor,
			"Nhà xe": Outdoor, "Hồ bơi": Outdoor, "Hiên": Outdoor,
		},
		// Thai is written without spaces between words, so every entry has
		// to match as a substring, like Chinese and Japanese.
		"thai": {
			"ห้องครัว": Kitchen, "ครัว": Kitchen, "ห้องอาหาร": Kitchen,
			"ห้องน้ำ": Bathroom, "ห้องอาบน้ำ": Bathroom, "ส้วม": Bathroom, "ฝักบัว": Bathroom,
			"ห้องนอน": Bedroom, "เตียง": Bedroom, "ห้องเด็ก": Bedroom,
			"ห้องทำงาน": Office, "สำนักงาน": Office, "โต๊ะทำงาน": Office, "ออฟฟิศ": Office,
			"ทางเดิน": Hallway, "บันได": Hallway, "ทางเข้า": Hallway,
			"ห้องนั่งเล่น": Living, "ห้องโถง": Living, "โซฟา": Living, "ทีวี": Living,
			"สวน": Outdoor, "ระเบียง": Outdoor, "เฉลียง": Outdoor, "โรงรถ": Outdoor,
			"สระว่ายน้ำ": Outdoor, "ลานบ้าน": Outdoor, "กลางแจ้ง": Outdoor,
		},
		"swahili": {
			"Jikoni": Kitchen, "Jiko": Kitchen, "Chumba cha kulia": Kitchen,
			"Bafu": Bathroom, "Bafuni": Bathroom, "Choo": Bathroom,
			"Chumba cha kuoga":  Bathroom,
			"Chumba cha kulala": Bedroom, "Kitanda": Bedroom,
			"Chumba cha wageni": Bedroom, "Chumba cha watoto": Bedroom,
			"Ofisi": Office, "Chumba cha kazi": Office, "Dawati": Office,
			"Karakana": Office,
			"Ukumbi":   Hallway, "Korido": Hallway, "Ngazi": Hallway, "Mlango": Hallway,
			"Sebule": Living, "Chumba cha kukaa": Living, "Kochi": Living,
			"Maktaba": Living,
			"Bustani": Outdoor, "Roshani": Outdoor, "Baraza": Outdoor,
			"Uwanja": Outdoor, "Gereji": Outdoor, "Bwawa": Outdoor, "Nje": Outdoor,
		},
		// Romanian is typed with comma-below letters (ș, ț) as often as with
		// the Turkish cedilla forms (ş, ţ); both fold the same way.
		"romanian": {
			"Bucătărie": Kitchen, "Bucatarie": Kitchen, "Sufragerie": Kitchen,
			"Sala de mese": Kitchen,
			"Baie":         Bathroom, "Duș": Bathroom, "Dus": Bathroom, "Grup sanitar": Bathroom,
			"Dormitor": Bedroom, "Noptieră": Bedroom, "Cameră de oaspeți": Bedroom,
			"Pat":   Bedroom,
			"Birou": Office, "Masă de lucru": Office,
			"Coridor": Hallway, "Intrare": Hallway, "Scară": Hallway,
			"Scări": Hallway, "Vestibul": Hallway,
			"Living": Living, "Șemineu": Living, "Salon": Living,
			"Grădină": Outdoor, "Curte": Outdoor, "Garaj": Outdoor,
			"Foișor": Outdoor, "Terasă": Outdoor, "Balcon": Outdoor,
		},
		"turkish": {
			"Mutfak": Kitchen, "Yemek odası": Kitchen,
			"Banyo": Bathroom, "Tuvalet": Bathroom, "Duş": Bathroom,
			"Yatak odası": Bedroom, "Yatak": Bedroom, "Çocuk odası": Bedroom,
			"Bebek odası": Bedroom,
			"Ofis":        Office, "Çalışma odası": Office, "Çalışma masası": Office,
			"Atölye": Office, "Büro": Office,
			"Koridor": Hallway, "Merdiven": Hallway, "Giriş": Hallway,
			"Antre": Hallway, "Hol": Hallway, "Sahanlık": Hallway,
			"Oturma odası": Living, "Misafir odası": Living, "Kanepe": Living,
			"Şömine": Living, "Kütüphane": Living, "Salon": Living,
			"Bahçe": Outdoor, "Havuz": Outdoor, "Avlu": Outdoor,
			"Garaj": Outdoor, "Balkon": Outdoor, "Teras": Outdoor,
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

		// zh
		"家庭影院": Other,   // home cinema; 庭 on its own is a garden
		"洗衣机":  Other,   // a washing machine, not a room
		"客房":   Bedroom, // a guest room, next to 客厅, the living room
		"餐厅":   Kitchen, // dining, next to 客厅, which shares 厅
		// ja
		"玄関前": Outdoor, // in front of the entrance; 玄関 alone is the hallway
		"客間":  Bedroom, // a guest room, next to 居間, the living room
		// ru
		"Подушка":          Other,    // a pillow; contains "душ", a shower
		"Диванная":         Living,   // a sofa room; contains "ванная", a bathroom
		"Гостевая комната": Bedroom,  // guest, next to "гостиная", the living room
		"Столовая":         Kitchen,  // dining; contains "стол", a table
		"Ванная комната":   Bathroom, //
		// ar
		"صالة الطعام": Kitchen, // a dining hall; صالة alone is a living room
		"غرفة الضيوف": Bedroom, // a guest room, not غرفة الجلوس, the sitting room
		// ur/hi
		"کھانے کا کمرہ": Kitchen, // contains کمرہ, which is just "room"
		"सोने का कमरा":  Bedroom, // likewise कमरा

		// vi. "đèn" is simply "lamp", and folds onto what used to be the
		// English living-room word "den"; it must not drag a lamp into the
		// living room on its own, but still has to place the compounds.
		"Đèn 1":           Other,
		"Den":             Other,
		"Đèn ngủ":         Bedroom,
		"Đèn bàn":         Office,
		"Đèn ban công":    Outdoor, // Outdoor is matched before Office
		"Đèn phòng khách": Living,
		"Phòng khách":     Living,   // literally "guest room", but the living room
		"Phòng tập":       Other,    // a gym; one letter from "phòng tắm"
		"Phòng tắm":       Bathroom, //
		// sw
		"Chumba cha kufulia": Other,   // laundry; next to "chumba cha kulia"
		"Chumba cha kulia":   Kitchen, //
		"Mlango":             Hallway, // a door; contains "lango", a gate
		// ro
		"Camera de zi":      Other,    // see the utility pattern: a living room
		"Cameră de oaspeți": Bedroom,  // a guest room, via the Italian "camera"
		"Sala de mese":      Kitchen,  // contains "sala", a living room
		"Spălătorie":        Other,    // laundry
		"Baie":              Bathroom, //
		// tr
		"Misafir odası": Living,  // the formal sitting room, not a guest bedroom
		"Çamaşır odası": Other,   // laundry
		"Yemek odası":   Kitchen, //
		"Yatak odası":   Bedroom, //
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

		// A Japanese flat is usually described by its combined living-dining
		// room, and the dining half wins because Kitchen is matched first.
		// Italian "sala da pranzo e soggiorno" has always resolved the same
		// way. Living would arguably model the presence better, so this is
		// the test to change if it bothers anyone.
		"リビングダイニング": Kitchen,

		// A library contains مكتب, a desk. Hue has no library archetype and
		// the routines have no library either, so Office is the closer of
		// the two available answers — English "library" goes to Living.
		"مكتبة": Office,

		// "pat" is a Romanian bed, which makes an English room named after
		// a person called Pat a bedroom. Harmless as often as not, and the
		// alternative is losing "Lampă pat".
		"Pat's room": Bedroom,
	}

	for name, want := range tests {
		if got := ClassifyRoom(name); got != want {
			t.Errorf("ClassifyRoom(%q) = %q, want the documented %q", name, got, want)
		}
	}
}

// TestRoomPatternKeywordsAreFolded is the guard that makes the vocabulary
// maintainable. Names are folded before they are matched, so a keyword that
// is not already written in folded form can never match anything — silently,
// and invisibly on screen when the difference is an Arabic alef variant or a
// Devanagari nukta.
func TestRoomPatternKeywordsAreFolded(t *testing.T) {
	for _, pattern := range roomPatterns {
		for _, keyword := range append(append([]string{}, pattern.contains...), pattern.words...) {
			if keyword == "" {
				t.Errorf("%q has an empty keyword", pattern.kind)
				continue
			}
			if folded := foldName(keyword); folded != keyword {
				t.Errorf("%q keyword %q is not in folded form; write it as %q (%s -> %s)",
					pattern.kind, keyword, folded, codepoints(keyword), codepoints(folded))
			}
		}
	}
}

// TestRoomPatternWordsAreSingleTokens checks the other half of that contract:
// a keyword in words is compared against whole tokens, so one containing a
// separator could never equal a token and belongs in contains instead.
func TestRoomPatternWordsAreSingleTokens(t *testing.T) {
	for _, pattern := range roomPatterns {
		for _, word := range pattern.words {
			if tokens := tokenize(word); len(tokens) != 1 || tokens[0] != word {
				t.Errorf("%q word %q is not a single token (%q); it belongs in contains",
					pattern.kind, word, tokens)
			}
		}
	}
}

func codepoints(value string) string {
	var out strings.Builder
	for i, r := range []rune(value) {
		if i > 0 {
			out.WriteByte(' ')
		}
		fmt.Fprintf(&out, "U+%04X", r)
	}
	return out.String()
}

func TestClassifyNameReportsUnknownNames(t *testing.T) {
	for _, name := range []string{
		"", "   ", "Zimmer 3", "Erdgeschoss", "Hue", "Lampe 1",
		// Utility rooms the vocabulary has no category for. They have to
		// report no match so a zone or the lamp's own name still gets a turn.
		"洗衣房", "储藏室", "洗濯室", "Прачечная", "Кладовая", "غرفة الغسيل", "مخزن",
		// Appliances and other non-rooms that sit near a keyword.
		"洗衣机", "洗濯機", "Пылесос", "مسلم", "ادراج",
		// Floors say nothing about what a room is used for.
		"一楼", "Первый этаж", "भूतल",
		// The same, in the languages added last: utility rooms, bare lamp
		// names, and floors.
		"Nhà kho", "Phòng giặt", "Tầng 1", "Đèn 1", // vi
		"ห้องซักผ้า", "ห้องเก็บของ", "ชั้น 1", "หลอดไฟ", // th
		"Chumba cha kufulia", "Stoo", "Taa 1", // sw
		"Spălătorie", "Debara", "Etajul 1", "Lampă 1", // ro
		"Çamaşır odası", "Kiler", "Birinci kat", "Lamba 1", // tr
	} {
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

		// ru
		"Кухня":   "кухня",
		"Ёлка":    "елка",
		"Спальня": "спальня",
		// ar: the vowel pointing is dropped, the alef variants collapse, and
		// the tatweel stretching character disappears.
		"غُرْفَة": "غرفة",
		"الأطفال": "الاطفال",
		"مرآب":    "مراب",
		"مـكـتـب": "مكتب",
		// hi: the nukta goes, whether it is written as its own character or
		// baked into a precomposed letter, and the candrabindu becomes the
		// anusvara it stands in for.
		"दफ़्तर": "दफ्तर",
		"बग़ीचा": "बगीचा",
		"आँगन":   "आंगन",
		// vi: the vowel and tone marks precompose to codepoints in the Latin
		// Extended Additional block, which folds by range.
		"Phòng ngủ":      "phong ngu",
		"Nhà bếp":        "nha bep",
		"Sân vườn":       "san vuon",
		"Phòng làm việc": "phong lam viec",
		"Đèn":            "den",
		// tr: the dotless i, and the dotted capital that Go lower-cases to a
		// plain "i".
		"Işık":          "isik",
		"Çalışma odası": "calisma odasi",
		"İç":            "ic",
		// ro: the comma-below and cedilla spellings meet.
		"Bucătărie": "bucatarie",
		"Șemineu":   "semineu",
		"Şemineu":   "semineu",
		"Duș":       "dus",
		// Scripts that are not folded at all pass through unchanged. Thai
		// vowels and tone marks are combining marks that carry the word, so
		// they have to survive.
		"ห้องน้ำ":      "ห้องน้ำ",
		"ห้องนั่งเล่น": "ห้องนั่งเล่น",
		"客厅":           "客厅",
		"リビング":         "リビング",
		"کمرہ":         "کمرہ",
	}

	for name, want := range tests {
		if got := foldName(name); got != want {
			t.Errorf("foldName(%q) = %q, want %q", name, got, want)
		}
	}
}
