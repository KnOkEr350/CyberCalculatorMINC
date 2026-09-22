package handlers

// INT-06: единственное место, где часы стажировки и практики приводятся к
// итоговым значениям. Запись может хранить либо готовый итог
// (`total_*_hours`), либо помесячную нагрузку и срок. Складывать оба
// представления нельзя — часы удвоятся, а вслед за ними и объём в
// регламентных формах, поэтому нормализация выполняется один раз и все
// потребители (расчёт объёмов Приложения № 4, Приложение № 2, отчёт по
// наставникам) обращаются сюда.

// studentHours — астрономические часы стажировки студента.
func studentHours(payload map[string]interface{}) float64 {
	if total := firstNumber(payload, "total_student_hours", "student_hours"); total > 0 {
		return total
	}
	return firstNumber(payload, "duration_months") *
		firstNumber(payload, "student_load_hours_per_month", "student_hours_per_month")
}

// mentorHours — астрономические часы сопровождения наставника. Ставка
// наставника по Приказу начисляется за каждого закреплённого стажёра
// персонально, поэтому часы считаются по записи стажёра.
func mentorHours(payload map[string]interface{}) float64 {
	if total := firstNumber(payload, "total_mentor_hours", "mentor_hours"); total > 0 {
		return total
	}
	return firstNumber(payload, "duration_months") *
		firstNumber(payload, "mentor_load_hours_per_month", "mentor_hours_per_month")
}
