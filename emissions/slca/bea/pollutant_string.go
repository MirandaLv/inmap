// Code generated by "stringer -type=Pollutant"; DO NOT EDIT.

package bea

import "strconv"

const _Pollutant_name = "PNH4PNO3PSO4SOAPrimaryPM25TotalPM25"

var _Pollutant_index = [...]uint8{0, 4, 8, 12, 15, 26, 35}

func (i Pollutant) String() string {
	if i < 0 || i >= Pollutant(len(_Pollutant_index)-1) {
		return "Pollutant(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Pollutant_name[_Pollutant_index[i]:_Pollutant_index[i+1]]
}