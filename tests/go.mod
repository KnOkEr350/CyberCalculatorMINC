module cybercalc/tests

go 1.27.1

require (
	cybercalc v0.0.0
	github.com/lib/pq v1.10.9
)

require github.com/klauspost/compress v1.20.0 // indirect

replace cybercalc => ../backend
