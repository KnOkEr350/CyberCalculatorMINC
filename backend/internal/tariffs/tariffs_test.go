package tariffs

import (
	"math/big"
	"regexp"
	"testing"
)

// Каждая ставка описана ровно один раз, с положительной суммой и понятным
// названием: иначе карточка молча перекрыла бы одно значение другим.
func TestDefinitionsAreUniqueAndComplete(t *testing.T) {
	seen := map[string]bool{}
	code := regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)+$`)
	for _, definition := range Definitions() {
		if seen[definition.Code] {
			t.Errorf("ставка %s описана дважды", definition.Code)
		}
		seen[definition.Code] = true
		if !code.MatchString(definition.Code) {
			t.Errorf("код %q не соответствует формату каталога", definition.Code)
		}
		if definition.Amount <= 0 {
			t.Errorf("%s: ставка должна быть положительной", definition.Code)
		}
		if definition.Label == "" || definition.Unit == "" || definition.Category == "" {
			t.Errorf("%s: у ставки нет названия, единицы или вида", definition.Code)
		}
	}
	if len(seen) != 22 {
		t.Fatalf("в редакции поставки %d ставок, ожидалось 22", len(seen))
	}
}

// Значения редакции поставки — это те суммы, что записаны в Методике. Тест
// фиксирует их отдельно от кода расчёта: подмена ставки правкой таблицы не
// должна проходить незамеченной.
func TestShippedEditionMatchesOrder270(t *testing.T) {
	want := map[string]int64{
		TeacherHourVuz: 4140, TeacherHourKolledj: 3900,
		InternshipStudentHour: 800, InternshipMentorHour: 2390,
		SchoolProgramHour: 4260, SchoolProgramDevelopment: 530890,
		TeacherTrainingHour: 3790, TeacherTrainingDevelopment: 1408570,
		PlatformStudentMonth: 6800, PlatformTeacherMonth: 8590,
		OODRPD("rpd", "vo", "development"):  300000,
		OODRPD("rpd", "vo", "update"):       160000,
		OODRPD("rpd", "vo", "expertise"):    55000,
		OODRPD("rpd", "spo", "development"): 270750,
		OODRPD("rpd", "spo", "update"):      150000,
		OODRPD("rpd", "spo", "expertise"):   58060,
		OODRPD("oop", "vo", "development"):  2039850,
		OODRPD("oop", "vo", "update"):       626110,
		OODRPD("oop", "vo", "expertise"):    312300,
		OODRPD("oop", "spo", "development"): 1731360,
		OODRPD("oop", "spo", "update"):      427440,
		OODRPD("oop", "spo", "expertise"):   171000,
	}
	card := Default()
	for code, amount := range want {
		got, err := card.Rat(code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if got.Cmp(big.NewRat(amount, 1)) != 0 {
			t.Errorf("%s: %s вместо %d", code, got.RatString(), amount)
		}
	}
	if len(want) != len(Definitions()) {
		t.Fatalf("проверено %d ставок из %d", len(want), len(Definitions()))
	}
}

// Незаданная ставка — ошибка, а не ноль: сумма без ставки хуже отказа.
func TestMissingRateIsAnErrorNotZero(t *testing.T) {
	card := Card{rates: map[string]Rate{}}
	if _, err := card.Rat(TeacherHourVuz); err == nil {
		t.Fatal("незаданная ставка должна давать ошибку")
	}
	if _, err := card.Float(TeacherHourVuz); err == nil {
		t.Fatal("незаданная ставка должна давать ошибку и в приближённом расчёте")
	}
	if _, err := Default().Rat("нет.такой.ставки"); err == nil {
		t.Fatal("неизвестная ставка должна давать ошибку")
	}
}

// Карточка отдаёт копию: расчёт не может испортить ставку для следующего.
func TestRatReturnsACopy(t *testing.T) {
	card := Default()
	first, _ := card.Rat(TeacherHourVuz)
	first.SetInt64(1)
	second, _ := card.Rat(TeacherHourVuz)
	if second.Cmp(big.NewRat(4140, 1)) != 0 {
		t.Fatal("правка полученной ставки изменила карточку")
	}
}

// Практика трудоустройства считается по ставкам стажировки; остальные виды
// используют собственные.
func TestCategoryCodesMapPracticeToInternship(t *testing.T) {
	internship := CategoryCodes("internship")
	practice := CategoryCodes("employment_practice")
	if len(internship) != 2 || len(practice) != 2 {
		t.Fatalf("у стажировки %d ставок, у практики %d, ожидалось по две", len(internship), len(practice))
	}
	for i := range internship {
		if internship[i] != practice[i] {
			t.Fatal("практика должна использовать ставки стажировки")
		}
	}
	if got := len(CategoryCodes("ood_rpd")); got != 12 {
		t.Fatalf("у «ООП и РПД» %d ставок, ожидалось 12", got)
	}
	// Виды без формулы по ставкам не имеют тарифов.
	for _, category := range []string{"top_it", "minc_decision", "нет-такого"} {
		if got := CategoryCodes(category); len(got) != 0 {
			t.Errorf("%s: неожиданные ставки %v", category, got)
		}
	}
}

// Основанием суммы служат версии, загруженные из БД; у ставок поставки версии
// нет, и в основание они не попадают.
func TestVersionIDsListOnlyLoadedVersions(t *testing.T) {
	card := Card{rates: map[string]Rate{
		InternshipStudentHour: {Code: InternshipStudentHour, Amount: big.NewRat(800, 1), VersionID: 9},
		InternshipMentorHour:  {Code: InternshipMentorHour, Amount: big.NewRat(2390, 1), VersionID: 4},
	}}
	got := card.VersionIDs("employment_practice")
	if len(got) != 2 || got[0] != 4 || got[1] != 9 {
		t.Fatalf("основание суммы %v, ожидалось [4 9]", got)
	}
	if ids := Default().VersionIDs("internship"); len(ids) != 0 {
		t.Fatalf("у карточки поставки нет версий БД, получено %v", ids)
	}
}

// Редакция выбирается по началу отчётного года.
func TestEffectiveDateIsTheStartOfTheYear(t *testing.T) {
	if got := EffectiveOn(2027).Format("2006-01-02"); got != "2027-01-01" {
		t.Fatalf("дата выбора редакции %s", got)
	}
}
