package controller

import (
	"context"
	"time"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	storagev1alpha1 "github.com/BenyaminSam/cephbridge/api/v1alpha1"
)

const (
	cephVolumeSnapshotPhasePending  = "Pending"
	cephVolumeSnapshotPhaseCreating = "Creating"
	cephVolumeSnapshotPhaseReady    = "Ready"
	cephVolumeSnapshotPhaseFailed   = "Failed"

	cephVolumeSnapshotConditionReady = "Ready"

	cephVolumeSnapshotDeletionPolicyDelete = "Delete"
	cephVolumeSnapshotDeletionPolicyRetain = "Retain"

	cephVolumeSnapshotFinalizer = "storage.infra.net/cephvolumesnapshot-finalizer"

	cephRBDVolumeSnapshotClass = "ceph-rbd-snapshot"
)

// CephVolumeSnapshotReconciler reconciles a CephVolumeSnapshot object
type CephVolumeSnapshotReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *CephVolumeSnapshotReconciler) handleDeletion(ctx context.Context, snapshot *storagev1alpha1.CephVolumeSnapshot) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.Info("Handling CephVolumeSnapshot deletion", "name", snapshot.Name)

	var volumeSnapshot snapshotv1.VolumeSnapshot

	err := r.Get(
		ctx,
		client.ObjectKey{
			Name:      snapshot.Name,
			Namespace: snapshot.Namespace,
		},
		&volumeSnapshot,
	)

	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("VolumeSnapshot already deleted", "name", snapshot.Name)
			controllerutil.RemoveFinalizer(snapshot, cephVolumeFinalizer)
			if err := r.Update(ctx, snapshot); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Deleting kubernetes VolumeSnapshot", "name", volumeSnapshot.Name)

	if err := r.Delete(ctx, &volumeSnapshot); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

// +kubebuilder:rbac:groups=storage.infra.net,resources=cephvolumesnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.infra.net,resources=cephvolumesnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.infra.net,resources=cephvolumesnapshots/finalizers,verbs=update
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.infra.net,resources=cephvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotcontents,verbs=get;list;watch

func (r *CephVolumeSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var snapshot storagev1alpha1.CephVolumeSnapshot

	if err := r.Get(ctx, req.NamespacedName, &snapshot); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconciling CephVolumeSnapshot", "name", snapshot.Name, "namespace", snapshot.Namespace)

	if !snapshot.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&snapshot, cephVolumeSnapshotFinalizer) {
			log.Info("CephVolumeSnapshot is being deleted")
			return r.handleDeletion(ctx, &snapshot)
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&snapshot, cephVolumeSnapshotFinalizer) {
		log.Info("Adding CephVolumeSnapshot finalizer")
		controllerutil.AddFinalizer(&snapshot, cephVolumeSnapshotFinalizer)
		if err := r.Update(ctx, &snapshot); err != nil {
			return ctrl.Result{}, err
		}
	}

	var cephVolume storagev1alpha1.CephVolume

	if err := r.Get(
		ctx,
		client.ObjectKey{
			Name:      snapshot.Spec.VolumeName,
			Namespace: snapshot.Namespace,
		},
		&cephVolume,
	); err != nil {
		if apierrors.IsNotFound(err) {
			return r.updateStatus(ctx, &snapshot, cephVolumeSnapshotPhaseFailed, "Referenced CephVolume was not found")
		}
		return ctrl.Result{}, err
	}

	var pvc corev1.PersistentVolumeClaim

	if err := r.Get(
		ctx,
		client.ObjectKey{
			Name:      cephVolume.Name,
			Namespace: cephVolume.Namespace,
		},
		&pvc,
	); err != nil {
		if apierrors.IsNotFound(err) {
			return r.updateStatus(
				ctx,
				&snapshot,
				cephVolumeSnapshotPhaseFailed,
				"PVC for CephVolume was not found",
			)
		}
		return ctrl.Result{}, err
	}

	var volumeSnapshot snapshotv1.VolumeSnapshot

	err := r.Get(
		ctx,
		client.ObjectKey{
			Name:      snapshot.Name,
			Namespace: snapshot.Namespace,
		},
		&volumeSnapshot,
	)

	if apierrors.IsNotFound(err) {
		log.Info("Creating kubernetes VolumeSnapshot", "name", snapshot.Name, "pvc", pvc.Name)

		volumeSnapshot = snapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snapshot.Name,
				Namespace: snapshot.Namespace,
			},
			Spec: snapshotv1.VolumeSnapshotSpec{
				VolumeSnapshotClassName: ptr(cephRBDVolumeSnapshotClass),
				Source: snapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: ptr(pvc.Name),
				},
			},
		}

		if err := ctrl.SetControllerReference(
			&snapshot,
			&volumeSnapshot,
			r.Scheme,
		); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, &volumeSnapshot); err != nil {
			return ctrl.Result{}, err
		}

		return r.updateStatus(ctx, &snapshot, cephVolumeSnapshotPhaseCreating, "Creating Kubernetes VolumeSnapshot")
	}

	if err != nil {
		return ctrl.Result{}, err
	}

	snapshot.Status.VolumeSnapshotName = volumeSnapshot.Name

	if volumeSnapshot.Status == nil {
		log.Info("VolumeSnapshot status not available yet", "name", volumeSnapshot.Name)
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	contentNamePtr := volumeSnapshot.Status.BoundVolumeSnapshotContentName
	if contentNamePtr != nil {
		contentName := *contentNamePtr

		var volumeSnapshotContent snapshotv1.VolumeSnapshotContent

		if err := r.Get(
			ctx,
			client.ObjectKey{Name: contentName},
			&volumeSnapshotContent,
		); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("VolumeSnapshotContent not found yet", "name", contentName)
				return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
			}
			return ctrl.Result{}, err
		}
		snapshot.Status.VolumeSnapshotContentName = contentName

		if volumeSnapshotContent.Status != nil && volumeSnapshotContent.Status.SnapshotHandle != nil {
			snapshot.Status.SnapshotHandle = *volumeSnapshotContent.Status.SnapshotHandle
		}
	}

	if volumeSnapshot.Status.ReadyToUse != nil {
		snapshot.Status.ReadyToUse = *volumeSnapshot.Status.ReadyToUse
	}

	if volumeSnapshot.Status.ReadyToUse != nil && *volumeSnapshot.Status.ReadyToUse {
		log.Info("Ceph volume snapshot is ready", "snapshot", volumeSnapshot.Name, "content", snapshot.Status.VolumeSnapshotContentName, "handle", snapshot.Status.SnapshotHandle)

		return r.updateStatus(ctx, &snapshot, cephVolumeSnapshotPhaseReady, "Ceph RBD snapshot is ready")
	}

	return ctrl.Result{}, nil
}

func (r *CephVolumeSnapshotReconciler) updateStatus(ctx context.Context, snapshot *storagev1alpha1.CephVolumeSnapshot, phase string, message string) (ctrl.Result, error) {

	snapshot.Status.Phase = phase
	snapshot.Status.Message = message
	snapshot.Status.ObservedGeneration = snapshot.Generation

	readyStatus := metav1.ConditionFalse

	if phase == cephVolumeSnapshotPhaseReady {
		readyStatus = metav1.ConditionTrue
	}

	meta.SetStatusCondition(
		&snapshot.Status.Conditions,
		metav1.Condition{
			Type:               cephVolumeSnapshotConditionReady,
			Status:             readyStatus,
			ObservedGeneration: snapshot.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             phase,
			Message:            message,
		},
	)

	if err := r.Status().Update(ctx, snapshot); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CephVolumeSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.CephVolumeSnapshot{}).
		Owns(&snapshotv1.VolumeSnapshot{}).
		Named("cephvolumesnapshot").
		Complete(r)
}
