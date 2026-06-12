package guardy

// FinishReport applies control-flow defaults and disposition to a manually constructed report.
func FinishReport(rep *Report, spec ControlSpec) *Report {
	if rep == nil {
		return nil
	}
	ApplyControlDefaults(rep, spec)
	rep.Disposition = DeriveDisposition(rep, nil)
	return rep
}
