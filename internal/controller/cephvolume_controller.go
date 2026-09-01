/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	storagev1alpha1 "github.com/BenyaminSam/cephbridge/api/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
)

const (
	cephVolumePhasePending  = "Pending"
	cephVolumePhaseCreating = "Creating"
	cephVolumePhaseReady    = "Ready"
	cephVolumePhaseFailed   = "Failed"

	cephVolumeConditionReady = "Ready"

	cephRBDStorageClass   = "ceph-rbd"
	cephRBDCSIProvisioner = "rook-ceph.rbd.csi.ceph.com"

	reasonProvisioning       = "Provisioning"
	reasonVolumeReady        = "VolumeReady"
	reasonProvisioningFailed = "ProvisioningFailed"

	cephVolumeFinalizer = "storage.infra.net/cephvolume-finalizer"

	cephVolumeDeletionPolicyDelete = "Delete"
	cephVolumeDeletionPolicyRetain = "Retain"
)

// CephVolumeReconciler reconciles a CephVolume object
type CephVolumeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *CephVolumeReconciler) handleDeletion(ctx context.Context, cephVolume *storagev1alpha1.CephVolume) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.Info("Handling CephVolume deletion", "name", cephVolume.Name)

	var pvc corev1.PersistentVolumeClaim

	err := r.Get(
		ctx,
		client.ObjectKey{
			Name:      cephVolume.Name,
			Namespace: cephVolume.Namespace,
		},
		&pvc,
	)

	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PVC already detected", "name", cephVolume.Name)

			controllerutil.RemoveFinalizer(cephVolume, cephVolumeFinalizer)

			if err := r.Update(ctx, cephVolume); err != nil {
				return ctrl.Result{}, err
			}

			log.Info("cephVolume finalizer removed", "name", cephVolume.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if pvc.Spec.VolumeName != "" {
		log.Info("PVC is bound during deletion", "pvc", pvc.Name, "pv", pvc.Spec.VolumeName)

		var pv corev1.PersistentVolume

		err := r.Get(
			ctx,
			types.NamespacedName{
				Name: pvc.Spec.VolumeName,
			},
			&pv,
		)

		if err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}

			log.Info("PV already deleted", "pv", pvc.Spec.VolumeName)
		} else {
			desiredReclaimPolicy := corev1.PersistentVolumeReclaimDelete

			if cephVolume.Spec.DeletionPolicy == cephVolumeDeletionPolicyRetain {
				desiredReclaimPolicy = corev1.PersistentVolumeReclaimRetain
			}

			if pv.Spec.PersistentVolumeReclaimPolicy != desiredReclaimPolicy {
				log.Info("Setting PV reclaim policy before PVC deletion", "pv", pv.Name, "from", pv.Spec.PersistentVolumeReclaimPolicy, "to", desiredReclaimPolicy)

				pv.Spec.PersistentVolumeReclaimPolicy = desiredReclaimPolicy

				if err := r.Update(ctx, &pv); err != nil {
					return ctrl.Result{}, err
				}

				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	log.Info("deleting PVC", "pvc", pvc.Name, "deletionPolicy", cephVolume.Spec.DeletionPolicy)

	if err := r.Delete(ctx, &pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("PVC deletion requested, waiting for CSI cleanup")
	return ctrl.Result{Requeue: true}, nil
}

// +kubebuilder:rbac:groups=storage.infra.net,resources=cephvolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.infra.net,resources=cephvolumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.infra.net,resources=cephvolumes/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;update;patch

func (r *CephVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cephVolume storagev1alpha1.CephVolume

	if err := r.Get(ctx, req.NamespacedName, &cephVolume); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconciling CephVolume", "name", cephVolume.Name, "namespace", cephVolume.Namespace)

	if cephVolume.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&cephVolume, cephVolumeFinalizer) {
			log.Info("Adding CephVolume finalizer")
			controllerutil.AddFinalizer(&cephVolume, cephVolumeFinalizer)
			if err := r.Update(ctx, &cephVolume); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(&cephVolume, cephVolumeFinalizer) {
			log.Info("CephVolume is being deleted")
			return r.handleDeletion(ctx, &cephVolume)
		}
		return ctrl.Result{}, nil
	}

	if cephVolume.Spec.Pool == "" {
		return r.updateStatus(
			ctx,
			&cephVolume,
			cephVolumePhaseFailed,
			"ceph pool must be specified",
		)
	}

	if cephVolume.Spec.Size.IsZero() {
		return r.updateStatus(
			ctx,
			&cephVolume,
			cephVolumePhaseFailed,
			"Ceph volume size must be greater than zero",
		)
	}

	var pvc corev1.PersistentVolumeClaim

	err := r.Get(
		ctx,
		client.ObjectKey{
			Name:      cephVolume.Name,
			Namespace: cephVolume.Namespace,
		},
		&pvc,
	)

	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if apierrors.IsNotFound(err) {
		log.Info("Creating PVC for CephVolume", "pvc", cephVolume.Name)

		pvc = corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cephVolume.Name,
				Namespace: cephVolume.Namespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr(cephRBDStorageClass),

				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},

				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: cephVolume.Spec.Size,
					},
				},
			},
		}

		if err := ctrl.SetControllerReference(
			&cephVolume,
			&pvc,
			r.Scheme,
		); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, &pvc); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return ctrl.Result{Requeue: true}, nil
			}

			return r.updateStatus(
				ctx,
				&cephVolume,
				cephVolumePhaseFailed,
				"Failed to create PVC: "+err.Error(),
			)
		}

		return r.updateStatus(
			ctx,
			&cephVolume,
			cephVolumePhaseCreating,
			"PVC created, waiting for Ceph CSI to provision the volume",
		)
	}

	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		return r.updateStatus(
			ctx,
			&cephVolume,
			cephVolumePhaseCreating,
			"Waiting for Ceph CSI to provision the volume",
		)
	case corev1.ClaimBound:
		log.Info("Ceph volume is ready", "pvc", pvc.Name, "pv", pvc.Spec.VolumeName)

		if pvc.Spec.VolumeName == "" {
			return r.updateStatus(
				ctx,
				&cephVolume,
				cephVolumePhaseCreating,
				"PVC is bound but PV is not available yet",
			)
		}

		var pv corev1.PersistentVolume

		if err := r.Get(
			ctx,
			types.NamespacedName{
				Name: pvc.Spec.VolumeName,
			},
			&pv,
		); err != nil {
			if apierrors.IsNotFound(err) {
				return r.updateStatus(
					ctx,
					&cephVolume,
					cephVolumePhaseCreating,
					"PVC is bound but PV has not been found",
				)
			}

			return ctrl.Result{}, err
		}

		desiredReclaimPolicy := corev1.PersistentVolumeReclaimDelete

		if cephVolume.Spec.DeletionPolicy == cephVolumeDeletionPolicyRetain {
			desiredReclaimPolicy = corev1.PersistentVolumeReclaimRetain
		}

		if pv.Spec.PersistentVolumeReclaimPolicy != desiredReclaimPolicy {
			log.Info("Updating PV reclaim policy", "pv", pv.Name, "from", pv.Spec.PersistentVolumeReclaimPolicy, "to", desiredReclaimPolicy)

			pv.Spec.PersistentVolumeReclaimPolicy = desiredReclaimPolicy

			if err := r.Update(ctx, &pv); err != nil {
				return ctrl.Result{}, err
			}

			return r.updateStatus(ctx, &cephVolume, cephVolumePhaseCreating, "Updating PV reclaim policy")
		}

		if pv.Spec.CSI == nil {
			return r.updateStatus(
				ctx,
				&cephVolume,
				cephVolumePhaseFailed,
				"Bound PV does not contain CSI information",
			)
		}

		if pv.Spec.CSI.Driver != cephRBDCSIProvisioner {
			return r.updateStatus(
				ctx,
				&cephVolume,
				cephVolumePhaseFailed,
				"PV uses unexpected CSI driver: "+pv.Spec.CSI.Driver,
			)
		}

		imageName := pv.Spec.CSI.VolumeAttributes["imageName"]
		pool := pv.Spec.CSI.VolumeAttributes["pool"]

		if imageName == "" {
			return r.updateStatus(
				ctx,
				&cephVolume,
				cephVolumePhaseFailed,
				"CSI PV does not contain Ceph image name",
			)
		}

		if pool == "" {
			return r.updateStatus(
				ctx,
				&cephVolume,
				cephVolumePhaseFailed,
				"CSI PV does not contain Ceph pool",
			)
		}

		cephVolume.Status.PVCName = pvc.Name
		cephVolume.Status.PVName = pv.Name
		cephVolume.Status.VolumeHandle = pv.Spec.CSI.VolumeHandle
		cephVolume.Status.ImageName = imageName
		cephVolume.Status.Pool = pool
		cephVolume.Status.Size = pvc.Status.Capacity.Storage().String()
		cephVolume.Status.ObservedGeneration = cephVolume.Generation

		return r.updateStatus(
			ctx,
			&cephVolume,
			cephVolumePhaseReady,
			"Ceph RBD volume is ready",
		)

	case corev1.ClaimLost:
		return r.updateStatus(
			ctx,
			&cephVolume,
			cephVolumePhaseFailed,
			"PVC has lost its bound volume",
		)
	}

	return ctrl.Result{}, nil
}

func (r *CephVolumeReconciler) updateStatus(ctx context.Context, cephVolume *storagev1alpha1.CephVolume, phase string, message string) (ctrl.Result, error) {
	cephVolume.Status.Phase = phase
	cephVolume.Status.Message = message
	cephVolume.Status.ObservedGeneration = cephVolume.Generation

	oldStatus := cephVolume.Status.DeepCopy()

	cephVolume.Status.Phase = phase
	cephVolume.Status.Message = message
	cephVolume.Status.ObservedGeneration = cephVolume.Generation

	readyStatus := metav1.ConditionFalse
	reason := reasonProvisioning

	switch phase {
	case cephVolumePhaseReady:
		readyStatus = metav1.ConditionTrue
		reason = reasonVolumeReady

	case cephVolumePhaseFailed:
		readyStatus = metav1.ConditionFalse
		reason = reasonProvisioningFailed

	case cephVolumePhaseCreating:
		readyStatus = metav1.ConditionFalse
		reason = reasonProvisioning
	}

	meta.SetStatusCondition(
		&cephVolume.Status.Conditions,
		metav1.Condition{
			Type:               cephVolumeConditionReady,
			Status:             readyStatus,
			ObservedGeneration: cephVolume.Generation,
			Reason:             reason,
			Message:            message,
		},
	)

	if reflect.DeepEqual(oldStatus, &cephVolume.Status) {
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, cephVolume); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func ptr[T any](value T) *T {
	return &value
}

var _ resource.Quantity

// SetupWithManager sets up the controller with the Manager.
func (r *CephVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.CephVolume{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Named("cephvolume").
		Complete(r)
}
