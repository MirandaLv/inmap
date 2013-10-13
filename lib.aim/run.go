package aim

import (
	"bitbucket.org/ctessum/sparse"
	"fmt"
	"math"
)

// Chemical mass conversions
const (
	// grams per mole
	mwNOx = 46.0055
	mwN   = 14.0067
	mwNO3 = 62.00501
	mwNH3 = 17.03056
	mwNH4 = 18.03851
	mwS   = 32.0655
	mwSO2 = 64.0644
	mwSO4 = 96.0632
	// ratios
	NOxToN = mwN / mwNOx
	NtoNO3 = mwNO3 / mwN
	SOxToS = mwSO2 / mwS
	StoSO4 = mwS / mwSO4
	NH3ToN = mwN / mwNH3
	NtoNH4 = mwNH4 / mwN
)

const NdaysToRun = 15. // Number of days of simulation time to run
const secondsPerDay = 1./3600./24.

// These are the names of pollutants accepted as emissions (μg/s)
var EmisNames = []string{"VOC", "NOx", "NH3", "SOx", "PM2_5"}

// These are the names of pollutants within the model
var polNames = []string{"gOrg", "pOrg", // gaseous and particulate organic matter
	"PM2_5",      // PM2.5
	"gNH", "pNH", // gaseous and particulate N in ammonia
	"gS", "pS", // gaseous and particulate S in sulfur
	"gNO", "pNO"} // gaseous and particulate N in nitrate

// Indicies of individual pollutants in arrays.
const (
	igOrg, ipOrg, iPM2_5, igNH, ipNH, igS, ipS, igNO, ipNO = 0, 1, 2, 3, 4, 5, 6, 7, 8
)

// These are the names of pollutants output by the model (μg/m3)
var OutputNames = []string{"VOC", "SOA", "PrimaryPM2_5", "NH3", "pNH4",
	"SOx", "pSO4", "NOx", "pNO3", "TotalPM2_5"}

// Run air quality model. Emissions are assumed to be in units
// of μg/s, and must only include the pollutants listed in "EmisNames".
func (m *MetData) Run(emissions map[string]*sparse.DenseArray) (
	outputConc map[string]*sparse.DenseArray) {

	// Emissions: all except PM2.5 go to gas phase
	emisFlux := make(map[string]*sparse.DenseArray)
	for pol, arr := range emissions {
		switch pol {
		case "VOC":
			emisFlux["gOrg"] = m.calcEmisFlux(arr, 1.)
		case "NOx":
			emisFlux["gNO"] = m.calcEmisFlux(arr, NOxToN)
		case "NH3":
			emisFlux["gNH"] = m.calcEmisFlux(arr, NH3ToN)
		case "SOx":
			emisFlux["gS"] = m.calcEmisFlux(arr, SOxToS)
		case "PM2_5":
			emisFlux["PM2_5"] = m.calcEmisFlux(arr, 1.)
		default:
			panic(fmt.Sprintf("Unknown emissions pollutant %v.", pol))
		}
	}

	// Initialize arrays
	// values at start of timestep
	initialConc := make([]*sparse.DenseArray, len(polNames))
	// values at end of timestep
	finalConc := make([]*sparse.DenseArray, len(polNames))
	// values at end of last timestep (to calculate convergence)
	oldFinalConcSum := make([]float64, len(polNames))
	for i, _ := range polNames {
		initialConc[i] = sparse.ZerosDense(m.Nz, m.Ny, m.Nx)
		finalConc[i] = sparse.ZerosDense(m.Nz, m.Ny, m.Nx)
	}

	polsConverged := make([]bool, len(polNames)) // whether pollutant arrays have converged.

	iteration := 0
	nDaysRun := 0.
	for {
		iteration++
		nDaysRun += m.Dt * secondsPerDay
		fmt.Printf("马上。。。Iteration %v; day %.5g\n", iteration,nDaysRun)

		// Add in emissions
		for i, pol := range polNames {
			if arr, ok := emisFlux[pol]; ok {
				initialConc[i].AddDense(arr)
			}
		}

		m.newRand() // set new random number

		// Calculate minimum value which is to be considered nonzero
		calcMin := max(initialConc[igOrg].Max(), initialConc[ipOrg].Max(),
			initialConc[iPM2_5].Max(),
			initialConc[igNH].Max(), initialConc[ipNH].Max(),
			initialConc[igS].Max(), initialConc[ipS].Max(),
			initialConc[igNO].Max(), initialConc[ipNO].Max()) * 1.e-6

		type empty struct{}
		sem := make(chan empty, m.Nz) // semaphore pattern
		for i := 1; i < m.Nx-1; i += 1 {
			go func(i int) { // concurrent processing
				var xadv, yadv, zadv, zdiff float64
				c := new(Neighborhood)
				d := new(Neighborhood)
				tempconc := make([]float64, len(polNames)) // concentration holder
				for j := 1; j < m.Ny-1; j += 1 {
					for k := 0; k < m.Nz; k += 1 {
						U := m.getBin(m.Ufreq, m.Ubins, k, j, i)
						Unext := m.getBin(m.Ufreq, m.Ubins, k, j, i+1)
						V := m.getBin(m.Vfreq, m.Vbins, k, j, i)
						Vnext := m.getBin(m.Vfreq, m.Vbins, k, j+1, i)
						W := m.getBin(m.Wfreq, m.Wbins, k, j, i)
						Wnext := m.getBin(m.Wfreq, m.Wbins, k+1, j, i)
						FillKneighborhood(d, m.verticalDiffusivity, k, j, i)
						for q, Carr := range initialConc {
							FillNeighborhood(c, Carr, m.Dz, k, j, i)
							if c.belowThreshold(calcMin) {
								continue
							}
							zdiff = m.DiffusiveFlux(c, d)
							xadv, yadv, zadv = m.AdvectiveFlux(c, U, Unext,
								V, Vnext, W, Wnext)

							var gravSettling float64
							var VOCoxidation float64
							switch q {
							case iPM2_5, ipOrg, ipNH, ipNO, ipS:
								gravSettling = m.GravitationalSettling(c, k)
							case igOrg:
								VOCoxidation = m.VOCoxidationFlux(c)
							}

							tempconc[q] = Carr.Get(k, j, i) +
								m.Dt*(xadv+yadv+zadv+gravSettling+VOCoxidation+
									zdiff)
						}
						m.WetDeposition(tempconc, k, j, i)
						m.ChemicalPartitioning(tempconc, k, j, i)

						for q, val := range tempconc {
							finalConc[q].Set(val, k, j, i)
						}
					}
				}
				sem <- empty{}
			}(i)
		}
		for i := 1; i < m.Nx-1; i++ { // wait for routines to finish
			<-sem
		}
		timeToQuit := true
		for q, polConverged := range polsConverged {
			arrSum := finalConc[q].Sum()
			if !polConverged {
				polsConverged[q] = checkConvergence(arrSum,
					oldFinalConcSum[q], polNames[q])
				if !polsConverged[q] {
					timeToQuit = false
				}
			}
			oldFinalConcSum[q] = arrSum
		}
		if timeToQuit && nDaysRun > NdaysToRun {
			break
		}
		for q, _ := range finalConc {
			initialConc[q] = finalConc[q].Copy()
			finalConc[q] = sparse.ZerosDense(m.Nz, m.Ny, m.Nx)
		}
	}
	outputConc = make(map[string]*sparse.DenseArray)
	outputConc["VOC"] = finalConc[igOrg]                       // gOrg
	outputConc["SOA"] = finalConc[ipOrg]                       // pOrg
	outputConc["PrimaryPM2_5"] = finalConc[iPM2_5]             // PM2_5
	outputConc["NH3"] = finalConc[igNH].ScaleCopy(1. / NH3ToN) // gNH
	outputConc["pNH4"] = finalConc[ipNH].ScaleCopy(NtoNH4)     // pNH
	outputConc["SOx"] = finalConc[igS].ScaleCopy(1. / SOxToS)  // gS
	outputConc["pSO4"] = finalConc[ipS].ScaleCopy(StoSO4)      // pS
	outputConc["NOx"] = finalConc[igNO].ScaleCopy(1. / NOxToN) // gNO
	outputConc["pNO3"] = finalConc[ipNO].ScaleCopy(NtoNO3)     // pNO
	outputConc["TotalPM2_5"] = finalConc[iPM2_5].Copy()
	outputConc["TotalPM2_5"].AddDense(outputConc["SOA"])
	outputConc["TotalPM2_5"].AddDense(outputConc["pNH4"])
	outputConc["TotalPM2_5"].AddDense(outputConc["pSO4"])
	outputConc["TotalPM2_5"].AddDense(outputConc["pNO3"])

	return
}

// Calculate emissions flux given emissions array in units of μg/s
// and a scale for molecular mass conversion.
func (m *MetData) calcEmisFlux(arr *sparse.DenseArray, scale float64) (
	emisFlux *sparse.DenseArray) {
	emisFlux = sparse.ZerosDense(m.Nz, m.Ny, m.Nx)
	for k := 0; k < m.Nz; k++ {
		for j := 0; j < m.Ny; j++ {
			for i := 0; i < m.Nx; i++ {
				fluxScale := 1. / m.Dx / m.Dy /
					m.Dz.Get(k, j, i) * m.Dt // μg/s /m/m/m * s = μg/m3
				emisFlux.Set(arr.Get(k, j, i)*scale*fluxScale, k, j, i)
			}
		}
	}
	return
}

func max(vals ...float64) float64 {
	m := 0.
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}

func checkConvergence(newSum, oldSum float64, name string) bool {
	bias := (newSum - oldSum) / oldSum
	fmt.Printf("%v: difference = %3.2g%%\n", name, bias*100)
	if bias > 0. || math.IsInf(bias,0) {
		return false
	} else {
		return true
	}
}